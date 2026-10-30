package handlers

// signal search: the public ISN signal search, and the private signal search (in one ISN, or across every ISN the caller can access)

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/utils"
)

// structs used by the search endpoint
type SearchParams struct {
	isnSlug                       string // from the per-ISN path - empty to search every ISN the caller can access
	signalTypeSlug                string // empty to search every signal type
	semVer                        string // empty to search every signal type
	accountID                     *uuid.UUID
	startDate                     *time.Time
	endDate                       *time.Time
	updatedSince                  *time.Time
	localRef                      *string
	issuerRef                     *string
	signalID                      *uuid.UUID
	correlationID                 *uuid.UUID
	includeWithdrawn              bool
	includeCorrelated             bool
	includePreviousSignalVersions bool
}

// search signals response

// SearchSignal is a signal returned by the search endpoints (the latest version of the signal).
//
// The field comments are the field descriptions in the API docs, so they refer to the json field names.
type SearchSignal struct {
	// the ISN the signal is in
	IsnSlug string `json:"isn_slug" example:"sample-isn"`

	// the account that sent the signal
	AccountID uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`

	// the type of account that sent the signal
	AccountType string `json:"account_type" example:"service_account" enums:"user,service_account"`

	// the email address of the account that sent the signal (the user's email, or the service account's contact email). Not included in public ISN searches
	Email string `json:"email,omitempty" example:"sender@example.com"`

	// the server generated ID of the signal. Use it as the correlation_id when submitting signals that link to this signal, and as the signal_id search filter
	SignalID uuid.UUID `json:"signal_id" example:"4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19"`

	// the sender's reference for the signal - unique for the sending account, ISN and signal type
	LocalRef string `json:"local_ref" example:"item_id_#1"`

	// the reference given by the organization that issued the thing the signal represents (e.g. an export health certificate number), if the sender supplied one.
	// Not unique - use it with the issuer_ref search filter. Currently only supported for documents
	IssuerRef *string `json:"issuer_ref,omitempty" example:"EHC-2026-000123"`

	// the signal type (use signal_type_slug and sem_ver, not the search URL, to build the URLs of correlated signals - they are often a different type)
	SignalTypeSlug string `json:"signal_type_slug" example:"sample-signal-type"`

	// the version of the signal type
	SemVer string `json:"sem_ver" example:"0.0.1"`

	// how the content is sent and stored: json (the content is the signal), document (the content is the document metadata - the document is downloaded from the content endpoint) or event (the content is the event, including occurred_at)
	ContentKind string `json:"content_kind" example:"json" enums:"json,document,event"`

	// when the signal was first received
	SignalCreatedAt time.Time `json:"signal_created_at" example:"2026-09-27T14:02:00.123456Z"`

	// when the signal last changed: a new version was received, or the signal was withdrawn (this is the cursor used when polling)
	SignalUpdatedAt time.Time `json:"signal_updated_at" example:"2026-09-27T14:02:00.123456Z"`

	// the server generated ID of this version of the signal
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`

	// the version number of this version (1 for the first version, incremented each time the signal is changed)
	VersionNumber int32 `json:"version_number" example:"1"`

	// when this version was received
	VersionCreatedAt time.Time `json:"version_created_at" example:"2026-09-27T14:02:00.123456Z"`

	// the signal_id of the signal this signal is linked to (the correlation_id it was submitted with). Null if the signal is not linked to another signal.
	// To fetch the linked signal, use GET /api/signals/search?signal_id=<correlation_id> (the linked signal is often a different type)
	CorrelationID *uuid.UUID `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca" extensions:"x-nullable"`

	// true if the signal has been withdrawn (withdrawn signals are only returned when include_withdrawn=true)
	IsWithdrawn bool `json:"is_withdrawn" example:"false"`

	// the signal content: the json payload for json signals and events, or the document metadata (name, mime_type, size_bytes and sha256) for documents
	Content json.RawMessage `json:"content" swaggertype:"object"`
}

// PreviousSignalVersion is an earlier version of a returned signal (include_previous_versions=true)
type PreviousSignalVersion struct {
	// the server generated ID of this version
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`

	// when this version was received
	CreatedAt time.Time `json:"created_at" example:"2026-09-27T14:02:00.123456Z"`

	// the version number of this version
	VersionNumber int32 `json:"version_number" example:"1"`

	// the content of this version (the json payload, or the document metadata for documents)
	Content json.RawMessage `json:"content" swaggertype:"object"`
}

// SearchSignalWithCorrelationsAndVersions is a search result, optionally with its correlated signals and previous versions
type SearchSignalWithCorrelationsAndVersions struct {
	SearchSignal

	// the signals linked to this signal - those whose correlation_id is this signal's signal_id (include_correlated=true).
	// Correlation is one level deep: the signals linked to these signals are not included
	CorrelatedSignals []SearchSignal `json:"correlated_signals,omitempty"`

	// the earlier versions of this signal (include_previous_versions=true)
	PreviousSignalVersions []PreviousSignalVersion `json:"previous_signal_versions,omitempty"`
}

// parseSearchParams parses all search parameters from the signal search request
func parseSearchParams(r *http.Request) (SearchParams, error) {
	searchParams := SearchParams{
		isnSlug:                       r.PathValue("isn_slug"),
		signalTypeSlug:                r.PathValue("signal_type_slug"),
		semVer:                        r.PathValue("sem_ver"),
		includeWithdrawn:              false,
		includeCorrelated:             false,
		includePreviousSignalVersions: false,
	}

	// account_id
	if accountIDString := r.URL.Query().Get("account_id"); accountIDString != "" {
		accountID, err := uuid.Parse(accountIDString)
		if err != nil {
			return searchParams, fmt.Errorf("account_id is not a valid UUID")
		}
		searchParams.accountID = &accountID
	}

	// start_date
	if startDateString := r.URL.Query().Get("start_date"); startDateString != "" {
		startDate, err := utils.ParseDateTime(startDateString)
		if err != nil {
			return searchParams, err
		}
		searchParams.startDate = &startDate
	}

	// end_date
	if endDateString := r.URL.Query().Get("end_date"); endDateString != "" {
		endDate, err := utils.ParseDateTime(endDateString)
		if err != nil {
			return searchParams, err
		}
		searchParams.endDate = &endDate
	}

	// updated_since
	if updatedSinceString := r.URL.Query().Get("updated_since"); updatedSinceString != "" {
		updatedSince, err := utils.ParseDateTime(updatedSinceString)
		if err != nil {
			return searchParams, err
		}
		searchParams.updatedSince = &updatedSince
	}

	// signal_id
	if signalIDString := r.URL.Query().Get("signal_id"); signalIDString != "" {
		signalID, err := uuid.Parse(signalIDString)
		if err != nil {
			return searchParams, fmt.Errorf("signal_id is not a valid UUID")
		}
		searchParams.signalID = &signalID
	}

	// correlation_id
	if correlationIDString := r.URL.Query().Get("correlation_id"); correlationIDString != "" {
		correlationID, err := uuid.Parse(correlationIDString)
		if err != nil {
			return searchParams, fmt.Errorf("correlation_id is not a valid UUID")
		}
		searchParams.correlationID = &correlationID
	}

	// local_ref
	if localRef := r.URL.Query().Get("local_ref"); localRef != "" {
		searchParams.localRef = &localRef
	}

	// issuer_ref
	if issuerRef := r.URL.Query().Get("issuer_ref"); issuerRef != "" {
		searchParams.issuerRef = &issuerRef
	}

	// include_withdrawn
	if includeWithdrawnString := r.URL.Query().Get("include_withdrawn"); includeWithdrawnString != "" {
		searchParams.includeWithdrawn = includeWithdrawnString == "true"
	}

	// include_correlated
	if includeCorrelatedString := r.URL.Query().Get("include_correlated"); includeCorrelatedString != "" {
		searchParams.includeCorrelated = includeCorrelatedString == "true"
	}

	// include_previous_versions
	if includePreviousString := r.URL.Query().Get("include_previous_versions"); includePreviousString != "" {
		searchParams.includePreviousSignalVersions = includePreviousString == "true"
	}
	return searchParams, nil
}

// validateSearchParams confirms that the combination of search parameters is valid
func validateSearchParams(params SearchParams) error {
	hasPartialDateRange := (params.startDate != nil) != (params.endDate != nil)
	if hasPartialDateRange {
		return fmt.Errorf("you must supply both start_date and end_date search parameters")
	}

	hasDateRange := params.startDate != nil && params.endDate != nil
	hasAccount := params.accountID != nil
	hasSignalID := params.signalID != nil
	hasLocalRef := params.localRef != nil
	hasIssuerRef := params.issuerRef != nil
	hasCorrelationID := params.correlationID != nil
	hasUpdatedSince := params.updatedSince != nil

	if !hasDateRange && !hasAccount && !hasSignalID && !hasLocalRef && !hasIssuerRef && !hasCorrelationID && !hasUpdatedSince {
		return fmt.Errorf("you must supply a search parameter")
	}

	return nil
}

// getPreviousSignalVersions fetches all the previous versions for a set of signals and returns them as a map of signal_id to versions
func (s *SignalsHandler) getPreviousSignalVersions(ctx context.Context, signalIDs []uuid.UUID) (map[uuid.UUID][]PreviousSignalVersion, error) {
	if len(signalIDs) == 0 {
		return make(map[uuid.UUID][]PreviousSignalVersion), nil
	}

	previousSignalVersions, err := s.queries.GetPreviousSignalVersions(ctx, signalIDs)
	if err != nil {
		return nil, fmt.Errorf("error retrieving signal versions %v", err)
	}

	// Group signal versions by their signal_id
	result := make(map[uuid.UUID][]PreviousSignalVersion)
	for _, previousSignalVersion := range previousSignalVersions {
		previousVersion := PreviousSignalVersion{
			SignalVersionID: previousSignalVersion.SignalVersionID,
			CreatedAt:       previousSignalVersion.CreatedAt,
			VersionNumber:   previousSignalVersion.VersionNumber,
			Content:         previousSignalVersion.Content,
		}
		result[previousSignalVersion.SignalID] = append(result[previousSignalVersion.SignalID], previousVersion)
	}

	return result, nil
}

// getCorrelatedSignals fetches all signals that have a correlated_id that references one of the provided signal IDs - returns a map of signal_id to correlated signals
//
// Visibility is decided per ISN: every correlated signal is returned in the readableIsnSlugs ISNs.
// In the other (write-only) ISNs only the signals viewerAccountID can see are returned: its own signals, and all the signals correlated to its own signals.
// Email addresses are only included when includeEmail is true (they are not shown in public ISNs).
func (s *SignalsHandler) getCorrelatedSignals(ctx context.Context, signalIDs []uuid.UUID, params SearchParams, readableIsnSlugs []string, viewerAccountID *uuid.UUID, includeEmail bool) (map[uuid.UUID][]SearchSignal, error) {
	if len(signalIDs) == 0 {
		return make(map[uuid.UUID][]SearchSignal), nil
	}

	correlatedSignals, err := s.queries.GetSignalsByCorrelationIDs(ctx, database.GetSignalsByCorrelationIDsParams{
		CorrelationIds:   signalIDs,
		IncludeWithdrawn: &params.includeWithdrawn,
		ReadableIsnSlugs: readableIsnSlugs,
		ViewerAccountID:  viewerAccountID,
	})
	if err != nil {
		return nil, fmt.Errorf("error retrieving correlated signals %v", err)
	}

	// Group correlated signals by their correlation_id
	result := make(map[uuid.UUID][]SearchSignal)
	for _, signal := range correlatedSignals {
		email := ""
		if includeEmail {
			email = signal.Email
		}
		correlatedSignal := SearchSignal{
			IsnSlug:          signal.IsnSlug,
			AccountID:        signal.AccountID,
			AccountType:      signal.AccountType,
			Email:            email,
			SignalID:         signal.SignalID,
			LocalRef:         signal.LocalRef,
			IssuerRef:        signal.IssuerRef,
			SignalTypeSlug:   signal.SignalTypeSlug,
			SemVer:           signal.SemVer,
			ContentKind:      signal.ContentKind,
			SignalCreatedAt:  signal.SignalCreatedAt,
			SignalUpdatedAt:  signal.SignalUpdatedAt,
			SignalVersionID:  signal.SignalVersionID,
			VersionNumber:    signal.VersionNumber,
			VersionCreatedAt: signal.VersionCreatedAt,
			CorrelationID:    &signal.CorrelationID, // the query excludes uncorrelated signals
			IsWithdrawn:      signal.IsWithdrawn,
			Content:          signal.Content,
		}
		result[signal.CorrelationID] = append(result[signal.CorrelationID], correlatedSignal)
	}

	return result, nil
}

// SearchPublicSignals godocs
//
//	@Summary		Signal Search (public ISNs)
//	@Tags			Search Signals
//
//	@Description	Search for signals in public ISNs (no authentication required).
//	@Description
//	@Description	The endpoint returns the latest version of each signal (use `include_previous_versions=true` to see earlier versions).
//	@Description	Results are ordered by `signal_updated_at` (oldest first).
//	@Description
//	@Description	**Search parameters**
//	@Description
//	@Description	At least one of these is required: `start_date` and `end_date`, `updated_since`, `account_id`, `signal_id`, `local_ref`, `issuer_ref` or `correlation_id`.
//	@Description	`start_date` and `end_date` must be used together. They filter on when the signal's latest version was received (`version_created_at`).
//	@Description
//	@Description	`updated_since` filters on when the signal last changed (`signal_updated_at`): a new version was received, or the signal was withdrawn.
//	@Description
//	@Description	**Polling for changes**
//	@Description
//	@Description	Polling is at-least-once: use `updated_since` as a cursor, start each poll 1 minute before the last `signal_updated_at` you received, and skip the results you have already seen (the same `signal_id` and `signal_updated_at`).
//	@Description	Use `include_withdrawn=true`, so signals withdrawn since your last poll are returned with `is_withdrawn: true` rather than left out of the results.
//	@Description	See *Polling for changes* in the API overview for more details.
//
//	@Param			isn_slug					path		string	true	"ISN slug"																																																					example(sample-isn)
//	@Param			signal_type_slug			path		string	true	"Signal type slug"																																																			example(signal-type-1)
//	@Param			sem_ver						path		string	true	"Signal type version"																																																		example(1.0.0)
//	@Param			start_date					query		string	false	"Signals whose latest version was created at or after this time (use with end_date)"																																		example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"Signals whose latest version was created at or before this time (use with start_date)"																																		example(2006-01-02T15:15:00Z)
//	@Param			updated_since				query		string	false	"Signals that changed (a new version was received, or the signal was withdrawn) since this time (to poll for changes, use with include_withdrawn=true and set it 1 minute before the last signal_updated_at you received)"	example(2006-01-02T15:05:00Z)
//	@Param			account_id					query		string	false	"Account ID"																																																				example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Return the signal with this signal_id (e.g. the correlation_id of a signal of another type)"																																example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"																																																			example(item_id_#1)
//	@Param			issuer_ref					query		string	false	"Signals with this issuer_ref (the reference given by the issuer of the thing the signal represents, e.g. an export health certificate number - currently only supported for documents)"									example(EHC-2026-000123)
//	@Param			correlation_id				query		string	false	"Return the signals of this type that are linked to the signal with this signal_id (those whose correlation_id is this value)"																								example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"																																												example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"																																						example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"																																						example(true)
//
//	@Success		200							{array}		handlers.SearchSignalWithCorrelationsAndVersions
//	@Failure		400							{object}	responses.ErrorResponse	"invalid_url_param"
//	@Failure		404							{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		500							{object}	responses.ErrorResponse	"database_error"
//
//	@Router			/api/public/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search [get]
//
//	@Description
//	@Description	**Documents**
//	@Description
//	@Description	For json signals and events, the signal is returned in the `content` field.
//	@Description	Documents are files, so they are not included in the search results: the `content` field holds the document's metadata (`name`, `mime_type`, `size_bytes` and `sha256`).
//	@Description	Downloading the file requires an account on the ISN and an access token (see *Download a Document*).
//
// This function can be called without authentication. It will only return signals from public ISNs.
func (s *SignalsHandler) SearchPublicSignals(w http.ResponseWriter, r *http.Request) error {

	// Parse all search parameters
	searchParams, err := parseSearchParams(r)
	if err != nil {
		return apperrors.InvalidURLParam("invalid search parameters", err)
	}

	signalTypePath := fmt.Sprintf("%v/v%v", searchParams.signalTypeSlug, searchParams.semVer)

	// Validate this is a public ISN
	if !s.publicIsnCache.HasSignalType(searchParams.isnSlug, signalTypePath) {
		logger.AddLogAttrs(r.Context(),
			slog.String("signal_type", signalTypePath),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		return apperrors.NotFound("signal type not available on this ISN", nil)
	}

	// Validate search parameters
	if err := validateSearchParams(searchParams); err != nil {
		return apperrors.InvalidURLParam("invalid search parameters", err)
	}

	// everyone can read every signal in a public ISN
	returnedSignals, err := s.queries.GetSignalsWithOptionalFilters(r.Context(), database.GetSignalsWithOptionalFiltersParams{
		IsnSlugs:         []string{searchParams.isnSlug},
		ReadableIsnSlugs: []string{searchParams.isnSlug},
		SignalTypeSlug:   &searchParams.signalTypeSlug,
		SemVer:           &searchParams.semVer,
		StartDate:        searchParams.startDate,
		EndDate:          searchParams.endDate,
		UpdatedSince:     searchParams.updatedSince,
		AccountID:        searchParams.accountID,
		SignalID:         searchParams.signalID,
		CorrelationID:    searchParams.correlationID,
		LocalRef:         searchParams.localRef,
		IssuerRef:        searchParams.issuerRef,
		IncludeWithdrawn: &searchParams.includeWithdrawn,
	})
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	response := make([]SearchSignalWithCorrelationsAndVersions, 0, len(returnedSignals))

	// the (optional) signal_versions and correlated_signals fields are populated using separate queries and then merged into the response
	// ...not very efficient but assumption is that these options will most likely be used with individual signals rather than in bulk (needs monitoring to confirm)
	signalIDs := make([]uuid.UUID, 0, len(returnedSignals))

	if searchParams.includeCorrelated || searchParams.includePreviousSignalVersions {
		for _, row := range returnedSignals {
			signalIDs = append(signalIDs, row.SignalID)
		}
	}

	// Get correlated signals if requested
	var correlatedSignalBySignalID map[uuid.UUID][]SearchSignal
	if searchParams.includeCorrelated {

		// create a map of signal_id to their correlated signals
		correlatedSignalBySignalID, err = s.getCorrelatedSignals(r.Context(), signalIDs, searchParams, []string{searchParams.isnSlug}, nil, false)
		if err != nil {
			return apperrors.DatabaseError("database error", err)
		}
	}
	var previousVersionsBySignalID map[uuid.UUID][]PreviousSignalVersion
	if searchParams.includePreviousSignalVersions {
		previousVersionsBySignalID, err = s.getPreviousSignalVersions(r.Context(), signalIDs)
		if err != nil {
			return apperrors.DatabaseError("database error", err)
		}
	}

	for _, returnedSignal := range returnedSignals {
		// uncorrelated signals are stored correlated to themselves - these are returned as null
		var correlationID *uuid.UUID
		if returnedSignal.CorrelationID != returnedSignal.SignalID {
			correlationID = &returnedSignal.CorrelationID
		}

		signal := SearchSignalWithCorrelationsAndVersions{
			SearchSignal: SearchSignal{
				IsnSlug:          returnedSignal.IsnSlug,
				AccountID:        returnedSignal.AccountID,
				AccountType:      returnedSignal.AccountType,
				Email:            "", // do not show email addresses in public ISNs
				SignalID:         returnedSignal.SignalID,
				LocalRef:         returnedSignal.LocalRef,
				IssuerRef:        returnedSignal.IssuerRef,
				SignalTypeSlug:   returnedSignal.SignalTypeSlug,
				SemVer:           returnedSignal.SemVer,
				ContentKind:      returnedSignal.ContentKind,
				SignalCreatedAt:  returnedSignal.SignalCreatedAt,
				SignalUpdatedAt:  returnedSignal.SignalUpdatedAt,
				SignalVersionID:  returnedSignal.SignalVersionID,
				VersionNumber:    returnedSignal.VersionNumber,
				VersionCreatedAt: returnedSignal.VersionCreatedAt,
				CorrelationID:    correlationID,
				IsWithdrawn:      returnedSignal.IsWithdrawn,
				Content:          returnedSignal.Content,
			},
		}
		// Add correlated signals if requested
		if searchParams.includeCorrelated {
			if correlatedSignals, exists := correlatedSignalBySignalID[returnedSignal.SignalID]; exists {
				signal.CorrelatedSignals = correlatedSignals
			}
		}

		// add previous versions
		if searchParams.includePreviousSignalVersions {
			if previousVersions, exists := previousVersionsBySignalID[returnedSignal.SignalID]; exists {
				signal.PreviousSignalVersions = previousVersions
			}
		}

		response = append(response, signal)
	}
	return responses.JSON(w, http.StatusOK, response)
}

// SearchPrivateSignals godocs
//
//	@Summary		Signal Search (private ISNs)
//	@Tags			Search Signals
//
//	@Description	Search for signals in one private ISN (authentication required).
//	@Description	To search without naming the ISN, use *Signal Search (one signal type, across your ISNs)* or *Signal Search (all signal types, across your ISNs)*.
//	@Description
//	@Description	The endpoint returns the latest version of each signal (use `include_previous_versions=true` to see earlier versions).
//	@Description	Results are ordered by `signal_updated_at` (oldest first).
//	@Description
//	@Description	Accounts with read access can see all signals on the ISN.
//	@Description	Write-only accounts can only see the signals created by their own account, and the signals other accounts have correlated to them (including those returned with `include_correlated=true`).
//	@Description
//	@Description	**Search parameters**
//	@Description
//	@Description	At least one of these is required: `start_date` and `end_date`, `updated_since`, `account_id`, `signal_id`, `local_ref`, `issuer_ref` or `correlation_id`.
//	@Description	`start_date` and `end_date` must be used together. They filter on when the signal's latest version was received (`version_created_at`).
//	@Description
//	@Description	`updated_since` filters on when the signal last changed (`signal_updated_at`): a new version was received, or the signal was withdrawn.
//	@Description
//	@Description	**Polling for changes**
//	@Description
//	@Description	Polling is at-least-once: use `updated_since` as a cursor, start each poll 1 minute before the last `signal_updated_at` you received, and skip the results you have already seen (the same `signal_id` and `signal_updated_at`).
//	@Description	Use `include_withdrawn=true`, so signals withdrawn since your last poll are returned with `is_withdrawn: true` rather than left out of the results.
//	@Description	See *Polling for changes* in the API overview for more details.
//	@Description
//	@Description	**Documents**
//	@Description
//	@Description	For json signals and events, the signal data is returned in the `content` field.
//	@Description	Documents are files, so they are not included directly in the search results, instead the `content` field holds the document's metadata (`name`, `mime_type`, `size_bytes` and `sha256`), and the file is downloaded separately with *Download a Document*:
//	@Description	```
//	@Description	GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/{signal_id}/content?version={version_number}
//	@Description	```
//	@Description	The URL is built from the search result's `signal_type_slug`, `sem_ver`, `signal_id` and `version_number` (correlated documents are generally a different signal type from the one you searched).
//
//	@Param			isn_slug					path		string	true	"ISN slug"																																																					example(sample-isn)
//	@Param			signal_type_slug			path		string	true	"Signal type slug"																																																			example(signal-type-1)
//	@Param			sem_ver						path		string	true	"Signal type version"																																																		example(1.0.0)
//	@Param			start_date					query		string	false	"Signals whose latest version was created at or after this time (use with end_date)"																																		example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"Signals whose latest version was created at or before this time (use with start_date)"																																		example(2006-01-02T15:15:00Z)
//	@Param			updated_since				query		string	false	"Signals that changed (a new version was received, or the signal was withdrawn) since this time (to poll for changes, use with include_withdrawn=true and set it 1 minute before the last signal_updated_at you received)"	example(2006-01-02T15:05:00Z)
//	@Param			account_id					query		string	false	"Account ID"																																																				example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Return the signal with this signal_id (e.g. the correlation_id of a signal of another type)"																																example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"																																																			example(item_id_#1)
//	@Param			issuer_ref					query		string	false	"Signals with this issuer_ref (the reference given by the issuer of the thing the signal represents, e.g. an export health certificate number - currently only supported for documents)"									example(EHC-2026-000123)
//	@Param			correlation_id				query		string	false	"Return the signals of this type that are linked to the signal with this signal_id (those whose correlation_id is this value)"																								example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"																																												example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"																																						example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"																																						example(true)
//
//	@Success		200							{array}		handlers.SearchSignalWithCorrelationsAndVersions
//	@Failure		400							{object}	responses.ErrorResponse	"invalid_url_param"
//	@Failure		401							{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		403							{object}	responses.ErrorResponse	"forbidden"
//	@Failure		404							{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		500							{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search [get]
//
// This function serves the per-ISN route and the routes that search across the caller's ISNs (SearchSignalTypeAcrossIsns and SearchAllSignalsAcrossIsns):
// the routes without an ISN in the path search every ISN in the caller's access token, and the routes without a signal type in the path search every signal type.
func (s *SignalsHandler) SearchPrivateSignals(w http.ResponseWriter, r *http.Request) error {

	// Parse all search parameters
	searchParams, err := parseSearchParams(r)
	if err != nil {
		return apperrors.InvalidURLParam("invalid search parameters", err)
	}

	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		return apperrors.AuthenticationFailure("authentication required for private ISN access", nil)
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return apperrors.AuthenticationFailure("could not determine account from access token", nil)
	}

	// Validate search parameters
	if err := validateSearchParams(searchParams); err != nil {
		return apperrors.InvalidURLParam("invalid search parameters", err)
	}

	// Choose the ISNs to search:
	// the per-ISN route searches the ISN in the path (the middleware has checked the caller is a member of it),
	// the routes without an ISN in the path search every ISN in the caller's access token.
	// ISNs that aren't in use are not removed here - the queries leave them out.
	isnSlugsInScope := slices.Collect(maps.Keys(claims.IsnPerms))
	if searchParams.isnSlug != "" {
		isnSlugsInScope = []string{searchParams.isnSlug}
	}

	// Note what the caller can see in each ISN - the queries apply the rule per ISN:
	// - read access: every signal in the ISN (readableIsnSlugs)
	// - write-only: the signals the caller created, and the signals correlated to them (the viewer account rule applies to every ISN not in readableIsnSlugs)
	// (the account_id filter still means "created by": filtering on another account returns that account's signals correlated to the viewer's signals)
	var isnSlugs, readableIsnSlugs []string
	for _, isnSlug := range isnSlugsInScope {
		isnPerms := claims.IsnPerms[isnSlug]
		if isnPerms.CanRead {
			readableIsnSlugs = append(readableIsnSlugs, isnSlug)
		}
		if isnPerms.CanRead || isnPerms.CanWrite {
			isnSlugs = append(isnSlugs, isnSlug)
		}
	}

	// the routes without a signal type in the path search every signal type
	var signalTypeSlug, semVer *string
	if searchParams.signalTypeSlug != "" {
		signalTypeSlug = &searchParams.signalTypeSlug
		semVer = &searchParams.semVer
	}

	returnedSignals, err := s.queries.GetSignalsWithOptionalFilters(r.Context(), database.GetSignalsWithOptionalFiltersParams{
		IsnSlugs:         isnSlugs,
		ReadableIsnSlugs: readableIsnSlugs,
		ViewerAccountID:  &accountID,
		SignalTypeSlug:   signalTypeSlug,
		SemVer:           semVer,
		StartDate:        searchParams.startDate,
		EndDate:          searchParams.endDate,
		UpdatedSince:     searchParams.updatedSince,
		AccountID:        searchParams.accountID,
		SignalID:         searchParams.signalID,
		CorrelationID:    searchParams.correlationID,
		LocalRef:         searchParams.localRef,
		IssuerRef:        searchParams.issuerRef,
		IncludeWithdrawn: &searchParams.includeWithdrawn,
	})
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.Any("isn_slugs", isnSlugs),
		)

		return apperrors.DatabaseError("database error", err)
	}

	// the (optional) signal_versions and correlated_signals fields are populated using separate queries and then merged into the response
	signalIDs := make([]uuid.UUID, 0, len(returnedSignals))
	if searchParams.includeCorrelated || searchParams.includePreviousSignalVersions {
		for _, row := range returnedSignals {
			signalIDs = append(signalIDs, row.SignalID)
		}
	}

	// Get correlated signals if requested (correlation is within one ISN, so each correlated signal gets the visibility rule of the ISN it is in)
	var correlatedSignalBySignalID map[uuid.UUID][]SearchSignal
	if searchParams.includeCorrelated {
		correlatedSignalBySignalID, err = s.getCorrelatedSignals(r.Context(), signalIDs, searchParams, readableIsnSlugs, &accountID, true)
		if err != nil {
			return apperrors.DatabaseError("database error", err)
		}
	}

	// Get previous versions if requested (these don't depend on the viewer: they are only fetched for signals the caller can see)
	var previousVersionsBySignalID map[uuid.UUID][]PreviousSignalVersion
	if searchParams.includePreviousSignalVersions {
		previousVersionsBySignalID, err = s.getPreviousSignalVersions(r.Context(), signalIDs)
		if err != nil {
			return apperrors.DatabaseError("database error", err)
		}
	}

	response := make([]SearchSignalWithCorrelationsAndVersions, 0, len(returnedSignals))
	for _, returnedSignal := range returnedSignals {
		// uncorrelated signals are stored correlated to themselves - these are returned as null
		var correlationID *uuid.UUID
		if returnedSignal.CorrelationID != returnedSignal.SignalID {
			correlationID = &returnedSignal.CorrelationID
		}

		signal := SearchSignalWithCorrelationsAndVersions{
			SearchSignal: SearchSignal{
				IsnSlug:          returnedSignal.IsnSlug,
				AccountID:        returnedSignal.AccountID,
				AccountType:      returnedSignal.AccountType,
				Email:            returnedSignal.Email,
				SignalID:         returnedSignal.SignalID,
				LocalRef:         returnedSignal.LocalRef,
				IssuerRef:        returnedSignal.IssuerRef,
				SignalTypeSlug:   returnedSignal.SignalTypeSlug,
				SemVer:           returnedSignal.SemVer,
				ContentKind:      returnedSignal.ContentKind,
				SignalCreatedAt:  returnedSignal.SignalCreatedAt,
				SignalUpdatedAt:  returnedSignal.SignalUpdatedAt,
				SignalVersionID:  returnedSignal.SignalVersionID,
				VersionNumber:    returnedSignal.VersionNumber,
				VersionCreatedAt: returnedSignal.VersionCreatedAt,
				CorrelationID:    correlationID,
				IsWithdrawn:      returnedSignal.IsWithdrawn,
				Content:          returnedSignal.Content,
			},
		}

		// Add correlated signals if requested
		if searchParams.includeCorrelated {
			if correlatedSignals, exists := correlatedSignalBySignalID[returnedSignal.SignalID]; exists {
				signal.CorrelatedSignals = correlatedSignals
			}
		}

		// add previous versions
		if searchParams.includePreviousSignalVersions {
			if previousVersions, exists := previousVersionsBySignalID[returnedSignal.SignalID]; exists {
				signal.PreviousSignalVersions = previousVersions
			}
		}
		response = append(response, signal)
	}
	return responses.JSON(w, http.StatusOK, response)
}

// SearchSignalTypeAcrossIsns godocs
//
//	@Summary		Signal Search (one signal type, across your ISNs)
//	@Tags			Search Signals
//
//	@Description	Search for signals of one signal type in every ISN your account is a member of, without naming the ISN (authentication required).
//	@Description	This is the preferred way to search: you don't need to know which ISN a signal is in (for example, when signals are sent through the router).
//	@Description	To search every signal type, use *Signal Search (all signal types, across your ISNs)*.
//	@Description
//	@Description	It takes the same search parameters as *Signal Search (private ISNs)*.
//	@Description	Each result includes its `isn_slug`, `signal_type_slug` and `sem_ver`. Results are ordered by `signal_updated_at` (oldest first) across all the ISNs, so polling works as for a single ISN.
//	@Description
//	@Description	The visibility rules are the same as for a single ISN, and are applied separately in each ISN: if you have read access to one ISN and write-only access to another, you see every signal in the first, and only your own signals and the signals correlated to them in the second.
//	@Description
//	@Description	A `signal_id` in an ISN you are not a member of returns no results, the same as a `signal_id` that doesn't exist.
//	@Description	A signal type that isn't used in any of your ISNs also returns no results.
//	@Description
//	@Description	The ISNs searched are the ones in your access token, so an ISN you have just been added to is searched once you next get an access token.
//	@Description
//	@Description	At least one of these is required: `start_date` and `end_date`, `updated_since`, `account_id`, `signal_id`, `local_ref`, `issuer_ref` or `correlation_id`.
//	@Description	See *Polling for changes* in the API overview for more details.
//
//	@Param			signal_type_slug			path		string	true	"Signal type slug"																																																			example(signal-type-1)
//	@Param			sem_ver						path		string	true	"Signal type version"																																																		example(1.0.0)
//	@Param			start_date					query		string	false	"Signals whose latest version was created at or after this time (use with end_date)"																																		example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"Signals whose latest version was created at or before this time (use with start_date)"																																		example(2006-01-02T15:15:00Z)
//	@Param			updated_since				query		string	false	"Signals that changed (a new version was received, or the signal was withdrawn) since this time (to poll for changes, use with include_withdrawn=true and set it 1 minute before the last signal_updated_at you received)"	example(2006-01-02T15:05:00Z)
//	@Param			account_id					query		string	false	"Account ID"																																																				example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Return the signal with this signal_id (e.g. the correlation_id of a signal of another type)"																																example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"																																																			example(item_id_#1)
//	@Param			issuer_ref					query		string	false	"Signals with this issuer_ref (the reference given by the issuer of the thing the signal represents, e.g. an export health certificate number - currently only supported for documents)"									example(EHC-2026-000123)
//	@Param			correlation_id				query		string	false	"Return the signals of this type that are linked to the signal with this signal_id (those whose correlation_id is this value)"																								example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"																																												example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"																																						example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"																																						example(true)
//
//	@Success		200							{array}		handlers.SearchSignalWithCorrelationsAndVersions
//	@Failure		400							{object}	responses.ErrorResponse	"invalid_url_param"
//	@Failure		401							{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		500							{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/signal-types/{signal_type_slug}/v{sem_ver}/signals/search [get]
//
// This function exists to give the route its own API docs - the search is done by SearchPrivateSignals:
// the route has no ISN in the path, so every ISN in the caller's access token is searched.
func (s *SignalsHandler) SearchSignalTypeAcrossIsns(w http.ResponseWriter, r *http.Request) error {
	return s.SearchPrivateSignals(w, r)
}

// SearchAllSignalsAcrossIsns godocs
//
//	@Summary		Signal Search (all signal types, across your ISNs)
//	@Tags			Search Signals
//
//	@Description	Search for signals of every signal type in every ISN your account is a member of (authentication required).
//	@Description	Use it to look up a `signal_id` whose type and ISN you don't know (e.g. the `correlation_id` of an event), or to poll for changes to every signal type with one cursor.
//	@Description	To search one signal type, use *Signal Search (one signal type, across your ISNs)*.
//	@Description
//	@Description	It takes the same search parameters as *Signal Search (private ISNs)*.
//	@Description	Each result includes its `isn_slug`, `signal_type_slug` and `sem_ver`, so you can build further URLs (e.g. *Download a Document*) from it. Results are ordered by `signal_updated_at` (oldest first) across all the ISNs and signal types, so polling works as for a single ISN.
//	@Description	Signals of signal types that are added to your ISNs later are returned too, so skip the types you don't need.
//	@Description
//	@Description	The visibility rules are the same as for a single ISN, and are applied separately in each ISN: if you have read access to one ISN and write-only access to another, you see every signal in the first, and only your own signals and the signals correlated to them in the second.
//	@Description
//	@Description	A `signal_id` in an ISN you are not a member of returns no results, the same as a `signal_id` that doesn't exist.
//	@Description
//	@Description	The ISNs searched are the ones in your access token, so an ISN you have just been added to is searched once you next get an access token.
//	@Description
//	@Description	At least one of these is required: `start_date` and `end_date`, `updated_since`, `account_id`, `signal_id`, `local_ref`, `issuer_ref` or `correlation_id`.
//	@Description	See *Following signals* and *Polling for changes* in the API overview for more details.
//
//	@Param			start_date					query		string	false	"Signals whose latest version was created at or after this time (use with end_date)"																																		example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"Signals whose latest version was created at or before this time (use with start_date)"																																		example(2006-01-02T15:15:00Z)
//	@Param			updated_since				query		string	false	"Signals that changed (a new version was received, or the signal was withdrawn) since this time (to poll for changes, use with include_withdrawn=true and set it 1 minute before the last signal_updated_at you received)"	example(2006-01-02T15:05:00Z)
//	@Param			account_id					query		string	false	"Account ID"																																																				example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Return the signal with this signal_id (e.g. the correlation_id of another signal)"																																			example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"																																																			example(item_id_#1)
//	@Param			issuer_ref					query		string	false	"Signals with this issuer_ref (the reference given by the issuer of the thing the signal represents, e.g. an export health certificate number - currently only supported for documents)"									example(EHC-2026-000123)
//	@Param			correlation_id				query		string	false	"Return the signals that are linked to the signal with this signal_id (those whose correlation_id is this value)"																											example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"																																												example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"																																						example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"																																						example(true)
//
//	@Success		200							{array}		handlers.SearchSignalWithCorrelationsAndVersions
//	@Failure		400							{object}	responses.ErrorResponse	"invalid_url_param"
//	@Failure		401							{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		500							{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/signals/search [get]
//
// This function exists to give the route its own API docs - the search is done by SearchPrivateSignals:
// the route has no ISN or signal type in the path, so every signal type in every ISN in the caller's access token is searched.
func (s *SignalsHandler) SearchAllSignalsAcrossIsns(w http.ResponseWriter, r *http.Request) error {
	return s.SearchPrivateSignals(w, r)
}
