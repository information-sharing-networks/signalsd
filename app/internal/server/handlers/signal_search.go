package handlers

// signal search: the public and private ISN signal search endpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/utils"
	"github.com/jackc/pgx/v5"
)

// structs used by the search endpoint
type SearchParams struct {
	isnSlug                       string
	signalTypeSlug                string
	semVer                        string
	accountID                     *uuid.UUID
	startDate                     *time.Time
	endDate                       *time.Time
	updatedSince                  *time.Time
	localRef                      *string
	signalID                      *uuid.UUID
	correlationID                 *uuid.UUID
	includeWithdrawn              bool
	includeCorrelated             bool
	includePreviousSignalVersions bool
}

// search signals reponse

// SearchSignal is a signal returned by the search endpoints (the latest version of the signal).
//
// The field comments are the field descriptions in the API docs, so they refer to the json field names.
type SearchSignal struct {
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

	// the signal type (use signal_type_slug and sem_ver, not the search URL, to build the URLs of correlated signals - they are often a different type)
	SignalTypeSlug string `json:"signal_type_slug" example:"sample-signal-type"`

	// the version of the signal type
	SemVer string `json:"sem_ver" example:"0.0.1"`

	// how the content is sent and stored: json (the content is the signal), document (the content is the document metadata - the document is downloaded from the content endpoint) or event (the content is the event, including occurred_at)
	ContentKind string `json:"content_kind" example:"json" enums:"json,document,event"`

	// when the signal was first received
	SignalCreatedAt time.Time `json:"signal_created_at" example:"2026-09-27T14:02:00.123456Z"`

	// when the signal was last created, given a new version, recorrelated or withdrawn (the cursor when polling - start the next poll 1 minute before the last result's value)
	SignalUpdatedAt time.Time `json:"signal_updated_at" example:"2026-09-27T14:02:00.123456Z"`

	// the server generated ID of this version of the signal
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`

	// the version number of this version (1 for the first version, incremented each time the signal is changed)
	VersionNumber int32 `json:"version_number" example:"1"`

	// when this version was received
	VersionCreatedAt time.Time `json:"version_created_at" example:"2026-09-27T14:02:00.123456Z"`

	// the signal_id of the signal this signal is linked to (the correlation_id it was submitted with). Null if the signal is not linked to another signal.
	// To fetch the linked signal, search its signal type with signal_id=<correlation_id>
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

// validateSearchParams conforms that the combination of search parameters is valid
func validateSearchParams(params SearchParams) error {
	hasPartialDateRange := (params.startDate != nil) != (params.endDate != nil)
	if hasPartialDateRange {
		return fmt.Errorf("you must supply both start_date and end_date search parameters")
	}

	hasDateRange := params.startDate != nil && params.endDate != nil
	hasAccount := params.accountID != nil
	hasSignalID := params.signalID != nil
	hasLocalRef := params.localRef != nil
	hasCorrelationID := params.correlationID != nil
	hasUpdatedSince := params.updatedSince != nil

	if !hasDateRange && !hasAccount && !hasSignalID && !hasLocalRef && !hasCorrelationID && !hasUpdatedSince {
		return fmt.Errorf("you must supply a search parameter")
	}

	return nil
}

// getSignals version fetches all the previous versions for a set of signals and returns them as a map of signal_id to versions
func (s *SignalsHandler) getPreviousSignalVersions(ctx context.Context, signalIDs []uuid.UUID) (map[uuid.UUID][]PreviousSignalVersion, error) {
	if len(signalIDs) == 0 {
		return make(map[uuid.UUID][]PreviousSignalVersion), nil
	}

	previousSignalVersions, err := s.queries.GetPreviousSignalVersions(ctx, signalIDs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return make(map[uuid.UUID][]PreviousSignalVersion), nil // not all signals have versions
		}
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
// viewerAccountID limits the results to the signals that account can see (used for write-only accounts, nil = no restriction):
// its own signals, and all the signals correlated to its own signals.
// Email addresses are only included when includeEmail is true (they are not shown in public ISNs).
func (s *SignalsHandler) getCorrelatedSignals(ctx context.Context, signalIDs []uuid.UUID, params SearchParams, viewerAccountID *uuid.UUID, includeEmail bool) (map[uuid.UUID][]SearchSignal, error) {
	if len(signalIDs) == 0 {
		return make(map[uuid.UUID][]SearchSignal), nil
	}

	correlatedSignals, err := s.queries.GetSignalsByCorrelationIDs(ctx, database.GetSignalsByCorrelationIDsParams{
		CorrelationIds:   signalIDs,
		IncludeWithdrawn: &params.includeWithdrawn,
		ViewerAccountID:  viewerAccountID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err // not all signals have correlated signals
		}
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
			AccountID:        signal.AccountID,
			AccountType:      signal.AccountType,
			Email:            email,
			SignalID:         signal.SignalID,
			LocalRef:         signal.LocalRef,
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
//	@Tags			Signal Exchange
//
//	@Description	Search for signals in public ISNs (no authentication required).
//	@Description
//	@Description	Note the endpoint returns the latest version of each signal.
//	@Description
//	@Description	**Polling for changes**
//	@Description
//	@Description	Use updated_since with include_withdrawn=true. Polling is at-least-once: start each poll 1 minute before the last signal_updated_at you received, and skip the results you have already seen (the same signal_id and signal_updated_at).
//	@Description	See *Polling for changes* in the API overview for why the overlap is needed.
//
//	@Param			start_date					query		string	false	"Start date"																																																example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"End date"																																																	example(2006-01-02T15:15:00Z)
//	@Param			updated_since				query		string	false	"Signals created, given a new version, recorrelated or withdrawn since this time (to poll for changes, use with include_withdrawn=true and set it 1 minute before the last signal_updated_at you received)"	example(2006-01-02T15:05:00Z)
//	@Param			account_id					query		string	false	"Account ID"																																																example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Return the signal with this signal_id (e.g. the correlation_id of a signal of another type)"																												example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"																																															example(item_id_#1)
//	@Param			correlation_id				query		string	false	"Return the signals of this type that are linked to the signal with this signal_id (those whose correlation_id is this value)"																				example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"																																								example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"																																		example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"																																		example(true)
//
//	@Success		200							{array}		handlers.SearchSignalWithCorrelationsAndVersions
//	@Failure		400							{object}	responses.ErrorResponse	"invalid_url_param"
//	@Failure		404							{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		500							{object}	responses.ErrorResponse	"database_error"
//
//	@Router			/api/public/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search [get]
//
//	@Description
//	@Description	**Retrieving a signal from the search results**
//	@Description
//	@Description	For json signals (content_kind=json) and events (content_kind=event) the signal is returned in the content field.
//	@Description
//	@Description	For document signals (content_kind=document) the content field holds the document metadata (name, mime_type, size_bytes and sha256).
//	@Description	Downloading the document requires an account on the ISN and an access token - see the private signal search endpoint for how to download a document from the search results.
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

	returnedSignals, err := s.queries.GetSignalsWithOptionalFilters(r.Context(), database.GetSignalsWithOptionalFiltersParams{
		IsnSlug:          searchParams.isnSlug,
		SignalTypeSlug:   searchParams.signalTypeSlug,
		SemVer:           searchParams.semVer,
		StartDate:        searchParams.startDate,
		EndDate:          searchParams.endDate,
		UpdatedSince:     searchParams.updatedSince,
		AccountID:        searchParams.accountID,
		SignalID:         searchParams.signalID,
		CorrelationID:    searchParams.correlationID,
		LocalRef:         searchParams.localRef,
		IncludeWithdrawn: &searchParams.includeWithdrawn,
	})
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	response := make([]SearchSignalWithCorrelationsAndVersions, 0, len(returnedSignals))

	// the (optional) signal_versions and correlated_signals fields are populated using separte queries and then merged into the response
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
		correlatedSignalBySignalID, err = s.getCorrelatedSignals(r.Context(), signalIDs, searchParams, nil, false)
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
				AccountID:        returnedSignal.AccountID,
				AccountType:      returnedSignal.AccountType,
				Email:            "", // do not show email addresses in public ISNs
				SignalID:         returnedSignal.SignalID,
				LocalRef:         returnedSignal.LocalRef,
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
//	@Tags			Signal Exchange
//
//	@Description	Search for signals by date or account in private ISNs (authentication required - only accounts with read or write permissions to the ISN can access signals).
//	@Description
//	@Description	Note the endpoint returns the latest version of each signal.
//	@Description
//	@Description	Write-only accounts can only see the signals created by their own account, and the signals other accounts have correlated to them.
//	@Description	This also applies to correlated signals returned with include_correlated=true.
//	@Description
//	@Description	**Polling for changes**
//	@Description
//	@Description	Use updated_since with include_withdrawn=true. Polling is at-least-once: start each poll 1 minute before the last signal_updated_at you received, and skip the results you have already seen (the same signal_id and signal_updated_at).
//	@Description	See *Polling for changes* in the API overview for why the overlap is needed.
//
//	@Param			start_date					query		string	false	"Start date"																																																example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"End date"																																																	example(2006-01-02T15:15:00Z)
//	@Param			updated_since				query		string	false	"Signals created, given a new version, recorrelated or withdrawn since this time (to poll for changes, use with include_withdrawn=true and set it 1 minute before the last signal_updated_at you received)"	example(2006-01-02T15:05:00Z)
//	@Param			account_id					query		string	false	"Account ID"																																																example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Return the signal with this signal_id (e.g. the correlation_id of a signal of another type)"																												example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"																																															example(item_id_#1)
//	@Param			correlation_id				query		string	false	"Return the signals of this type that are linked to the signal with this signal_id (those whose correlation_id is this value)"																				example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"																																								example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"																																		example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"																																		example(true)
//
//	@Success		200							{array}		handlers.SearchSignalWithCorrelationsAndVersions
//	@Failure		400							{object}	responses.ErrorResponse	"invalid_url_param"
//	@Failure		401							{object}	responses.ErrorResponse	"authentication_error"
//	@Failure		500							{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search [get]
//
//	@Description
//	@Description	**Retrieving a signal from the search results**
//	@Description
//	@Description	For json signals (content_kind=json) and events (content_kind=event) the signal is returned in the content field.
//	@Description
//	@Description	For document signals (content_kind=document) the content field holds the document metadata (name, mime_type, size_bytes and sha256) and the document is downloaded from the signal's content endpoint:
//	@Description	```
//	@Description	GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/{signal_id}/content?version={version_number}
//	@Description	```
//	@Description	Build the URL from the signal_type_slug, sem_ver, signal_id and version_number in the search result, not from the search URL:
//	@Description	correlated signals (include_correlated=true) are often a different signal type from the one searched.
//	@Description	The isn_slug is the ISN that was searched (correlated signals are in the same ISN).
//	@Description
//	@Description	For example, searching for a shipment with its correlated signals:
//	@Description	```
//	@Description	GET /api/isn/sample-isn/signal-types/shipment/v1.0.0/signals/search?local_ref=SHIP-0042&include_correlated=true
//	@Description
//	@Description	[{
//	@Description	"signal_id": "def87f89-dab6-4607-95f7-593d61cb5742",
//	@Description	"signal_type_slug": "shipment",
//	@Description	"sem_ver": "1.0.0",
//	@Description	"content_kind": "json",
//	@Description	"correlation_id": null,
//	@Description	"content": {...},
//	@Description	"correlated_signals": [{
//	@Description	"signal_id": "4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19",
//	@Description	"version_number": 2,
//	@Description	"signal_type_slug": "bill-of-lading",
//	@Description	"sem_ver": "1.0.0",
//	@Description	"content_kind": "document",
//	@Description	"correlation_id": "def87f89-dab6-4607-95f7-593d61cb5742",
//	@Description	"content": {"name": "BL-2026-0042.pdf", "mime_type": "application/pdf", ...},
//	@Description	...
//	@Description	}],
//	@Description	...
//	@Description	}]
//	@Description	```
//	@Description	the correlated bill of lading is downloaded with:
//	@Description	```
//	@Description	GET /api/isn/sample-isn/signal-types/bill-of-lading/v1.0.0/signals/4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19/content?version=2
//	@Description	```
//	@Description	Omit version to download the latest version, and add include_withdrawn=true if the signal is withdrawn.
//
// This function should be called after the RequireAccessPermission middleware has checked the account has read permission for the ISN
// (the middleware also checks the isn and signal type are in use)
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

	// ISN and signal type in-use checks are now performed by RequireIsnPermission middleware

	// Validate search parameters
	if err := validateSearchParams(searchParams); err != nil {
		return apperrors.InvalidURLParam("invalid search parameters", err)
	}

	// Write-only accounts can only see the signals they created and the signals correlated to them
	// (the account_id filter still means "created by": filtering on another account returns that account's signals correlated to the viewer's signals).
	// The same restriction applies to correlated signals
	var writeOnlyAccountID *uuid.UUID
	isnPerms := claims.IsnPerms[searchParams.isnSlug]
	if !isnPerms.CanRead && isnPerms.CanWrite {
		accountID, ok := auth.ContextAccountID(r.Context())
		if !ok {
			return apperrors.AuthenticationFailure("could not determine account from access token", nil)
		}
		writeOnlyAccountID = &accountID
	}

	returnedSignals, err := s.queries.GetSignalsWithOptionalFilters(r.Context(), database.GetSignalsWithOptionalFiltersParams{
		IsnSlug:          searchParams.isnSlug,
		SignalTypeSlug:   searchParams.signalTypeSlug,
		SemVer:           searchParams.semVer,
		StartDate:        searchParams.startDate,
		EndDate:          searchParams.endDate,
		UpdatedSince:     searchParams.updatedSince,
		ViewerAccountID:  writeOnlyAccountID,
		AccountID:        searchParams.accountID,
		SignalID:         searchParams.signalID,
		CorrelationID:    searchParams.correlationID,
		LocalRef:         searchParams.localRef,
		IncludeWithdrawn: &searchParams.includeWithdrawn,
	})
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	response := make([]SearchSignalWithCorrelationsAndVersions, 0, len(returnedSignals))

	// the (optional) signal_versions and correlated_signals fields are populated using separte queries and then merged into the response
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
		correlatedSignalBySignalID, err = s.getCorrelatedSignals(r.Context(), signalIDs, searchParams, writeOnlyAccountID, true)
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
				AccountID:        returnedSignal.AccountID,
				AccountType:      returnedSignal.AccountType,
				Email:            returnedSignal.Email,
				SignalID:         returnedSignal.SignalID,
				LocalRef:         returnedSignal.LocalRef,
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
