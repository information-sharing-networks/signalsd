package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"slices"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server/isns"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SignalsHandler struct {
	queries        *database.Queries
	pool           *pgxpool.Pool
	schemaCache    *schemas.SchemaCache
	publicIsnCache *isns.PublicIsnCache
}

func NewSignalsHandler(queries *database.Queries, pool *pgxpool.Pool, schemaCache *schemas.SchemaCache, publicIsnCache *isns.PublicIsnCache) *SignalsHandler {
	return &SignalsHandler{
		queries:        queries,
		pool:           pool,
		schemaCache:    schemaCache,
		publicIsnCache: publicIsnCache,
	}
}

// CREATE SIGNALS

// create signals request json
type CreateSignal struct {
	LocalRef      string          `json:"local_ref" example:"item_id_#1"`
	CorrelationId *uuid.UUID      `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"` //optional - supply the id of another signal if you want to link to it
	Content       json.RawMessage `json:"content" swaggertype:"object"`
}

type CreateSignalsRequest struct {
	Signals []CreateSignal `json:"signals"`
}

// structs - partial loads are possible - c.f StoredSignal and FailedSignal
type CreateSignalsResponse struct {
	IsnSlug        string               `json:"isn_slug" example:"sample-isn--example-org"`
	SignalTypePath string               `json:"signal_type_path" example:"signal-type-1/v0.0.1"`
	AccountID      uuid.UUID            `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	SignalsBatchID uuid.UUID            `json:"signals_batch_id" example:"b51faf05-aaed-4250-b334-2258ccdf1ff2"`
	Results        CreateSignalsResults `json:"results"`
	Summary        CreateSignalsSummary `json:"summary"`
}

type CreateSignalsResults struct {
	StoredSignals []StoredSignal `json:"stored_signals"`
	FailedSignals []FailedSignal `json:"failed_signals,omitempty"`
}

type StoredSignal struct {
	LocalRef        string    `json:"local_ref" example:"item_id_#1"`
	SignalID        uuid.UUID `json:"signal_id" example:"b8ded113-ac0e-4a2c-a89f-0876fe97b440"`
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`
	VersionNumber   int32     `json:"version_number" example:"1"`
}

type FailedSignal struct {
	LocalRef     string `json:"local_ref" example:"item_id_#2"`
	ErrorCode    string `json:"error_code" example:"validation_error"`
	ErrorMessage string `json:"error_message" example:"field 'name' is required"`
}

type CreateSignalsSummary struct {
	TotalSubmitted int `json:"total_submitted" example:"100"`
	StoredCount    int `json:"stored_count" example:"95"`
	FailedCount    int `json:"failed_count" example:"5"`
}

// WITHDRAW SIGNALS

// withdraw signals request
type WithdrawSignalRequest struct {
	LocalRef *string `json:"local_ref,omitempty" example:"item_id_#1"`
}

// SEARCH SIGNALS

// structs used by the search endpoint
type SearchParams struct {
	isnSlug                       string
	signalTypeSlug                string
	semVer                        string
	accountID                     *uuid.UUID
	startDate                     *time.Time
	endDate                       *time.Time
	localRef                      *string
	signalID                      *uuid.UUID
	includeWithdrawn              bool
	includeCorrelated             bool
	includePreviousSignalVersions bool
}

// search signals reponse

// SearchSignal represents a signal returned from the database
type SearchSignal struct {
	AccountID            uuid.UUID       `json:"account_id"`
	AccountType          string          `json:"account_type"`
	Email                string          `json:"email,omitempty"` // not included in public ISN searches
	SignalID             uuid.UUID       `json:"signal_id"`
	LocalRef             string          `json:"local_ref"`
	SignalCreatedAt      time.Time       `json:"signal_created_at"`
	SignalVersionID      uuid.UUID       `json:"signal_version_id"`
	VersionNumber        int32           `json:"version_number"`
	VersionCreatedAt     time.Time       `json:"version_created_at"`
	CorrelatedToSignalID uuid.UUID       `json:"correlated_to_signal_id"`
	IsWithdrawn          bool            `json:"is_withdrawn"`
	Content              json.RawMessage `json:"content" swaggertype:"object"`
}

// optionally the search can return the history of a signal
type PreviousSignalVersion struct {
	SignalVersionID uuid.UUID       `json:"signal_version_id"`
	CreatedAt       time.Time       `json:"created_at"`
	VersionNumber   int32           `json:"version_number"`
	Content         json.RawMessage `json:"content" swaggertype:"object"`
}

// optionally the search can return the signals correlated with a returned signal
type SearchSignalWithCorrelationsAndVersions struct {
	SearchSignal
	CorrelatedSignals      []SearchSignal          `json:"correlated_signals,omitempty"`
	PreviousSignalVersions []PreviousSignalVersion `json:"previous_signal_versions,omitempty"`
}

type SearchSignalResponse struct {
	Signals []SearchSignalWithCorrelationsAndVersions `json:"signals"`
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

	// signal_id
	if signalIDString := r.URL.Query().Get("signal_id"); signalIDString != "" {
		signalID, err := uuid.Parse(signalIDString)
		if err != nil {
			return searchParams, fmt.Errorf("signal_id is not a valid UUID")
		}
		searchParams.signalID = &signalID
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

	if !hasDateRange && !hasAccount && !hasSignalID && !hasLocalRef {
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
func (s *SignalsHandler) getCorrelatedSignals(ctx context.Context, signalIDs []uuid.UUID, params SearchParams) (map[uuid.UUID][]SearchSignal, error) {
	if len(signalIDs) == 0 {
		return make(map[uuid.UUID][]SearchSignal), nil
	}

	correlatedSignals, err := s.queries.GetSignalsByCorrelationIDs(ctx, database.GetSignalsByCorrelationIDsParams{
		CorrelationIds:   signalIDs,
		IncludeWithdrawn: &params.includeWithdrawn,
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
		correlatedSignal := SearchSignal{
			AccountID:            signal.AccountID,
			Email:                signal.Email,
			SignalID:             signal.SignalID,
			LocalRef:             signal.LocalRef,
			SignalCreatedAt:      signal.SignalCreatedAt,
			SignalVersionID:      signal.SignalVersionID,
			VersionNumber:        signal.VersionNumber,
			VersionCreatedAt:     signal.VersionCreatedAt,
			CorrelatedToSignalID: signal.CorrelatedToSignalID,
			IsWithdrawn:          signal.IsWithdrawn,
			Content:              signal.Content,
		}
		result[signal.CorrelatedToSignalID] = append(result[signal.CorrelatedToSignalID], correlatedSignal)
	}

	return result, nil
}

// CreateSignalsHandler godocs
//
//	@Summary		Create signals
//	@Tags			Signal Exchange
//
//	@Description	Submit an array of signals for storage on the ISN
//	@Description	- payloads must not mix signals of different types and are subject to the size limits defined on the site.
//	@Description	- The client-supplied local_ref must uniquely identify each signal of the specified signal type that will be supplied by the account.
//	@Description	- If a local reference is received more than once from an account for the specified signal_type a new version of the signal will be stored with a incremented version number.
//	@Description	- Optionally a correlation_id can be supplied - this will link the signal to a previously received signal. The correlated signal does not need to be owned by the same account but must be in the same ISN.
//	@Description	- requests are only accepted for the open signal batch for this account/ISN (service accounts need to manually create batches, web users have a batch automatically created when they first write to an ISN).
//	@Description
//	@Description	**Authentication**
//	@Description
//	@Description	Requires a valid access token.
//	@Description	The claims in the access token list the ISNs and signal_types that the account is permitted to use.
//	@Description
//	@Description	**Error handling**
//	@Description
//	@Description	if the request is a vaild format but individual signals contain errors (validation errors, incorrect correlation ids, database errors) the errors are recorded in the response but do not prevent other signals from being processed.
//	@Description	Individual failures are logged and can be tracked using the signals_batch_id returned in the response - see the batch status endpoint.
//	@Description
//	@Description	Errors that relate to the entire request  - e.g invalid json, authentication, permission and server errors (400, 401, 403, 500) - are not recorded and should be handled by the client immediately.
//	@Description
//	@Description	**JSON Schema Validation**
//	@Description
//	@Description	Signals are validated against the JSON schema specified for the signal type unless validation is disabled on the type definition.
//	@Description
//	@Description	When validation is disabled, basic checks are still done on the incoming data and the following issues create a 400 error and cause the entire payload to be rejected:
//	@Description	- invalid json format
//	@Description	- missing fields (the array of signals must be in a json object called signals, and content and local_ref must be present for each record).
//	@Description
//	@Description	**Signal versions**
//	@Description
//	@Description	Multiple versions are created when signals are resupplied using the same local_ref, e.g. because the client wants to correct a previously publsihed signal.
//	@Description	By default search will return the latest version of the signal.
//	@Description	If a signal has been withdrawn it will be reactivated if you resubmit it using the same local_ref.
//	@Description
//	@Description	**Correlating signals**
//	@Description
//	@Description	Correlation IDs can be used to link signals together.  Signals can only be correlated within the same ISN.
//	@Description	If the supplied correlation_id is not found in the same ISN as the signal being submitted, the response will contain a 422 or 207 status code and the error_code for the failed signal will be `invalid_correlation_id`.
//	@Description
//	@Description	request level errors (e.g. invalid json, authentication failure etc) return a simple error_code/error_message response rather than a detailed audit log
//
//	@Param			isn_slug			path		string							true	"isn slug"						example(sample-isn--example-org)
//	@Param			signal_type_slug	path		string							true	"signal type slug"				example(sample-signal--example-org)
//	@Param			sem_ver				path		string							true	"signal type sem_ver number"	example(0.0.1)
//	@Param			request				body		handlers.CreateSignalsRequest	true	"create signals"
//
//	@Success		200					{object}	handlers.CreateSignalsResponse	"All signals processed successfully"
//	@Success		207					{object}	handlers.CreateSignalsResponse	"Partial success - some signals succeeded, some failed"
//	@Success		422					{object}	handlers.CreateSignalsResponse	"Valid request format but all signals failed processing - returns detailed error information"
//	@Failure		400					{object}	responses.ErrorResponse			"Invalid request format (error_code = malformed_body)
//	@Failure		401					{object}	responses.ErrorResponse			"Unauthorized request (invalid credentials, error_code = authentication_error)"
//	@Failure		403					{object}	responses.ErrorResponse			"Forbidden (no permission to write to ISN, error_code = forbidden)"
//	@Failure		404					{object}	responses.ErrorResponse			"Not Found (mistyped url or signal_type marked 'not in use')
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals [post]
//
// CreateSignal Handler inserts signals and signal_versions records - signals are the master records containing
// the local_ref and correlation_id, signal_versions contains the content and links back to the signal.
// the handler will record errors encountered when processing individual signals (see the signal_processing_failures table).
func (s *SignalsHandler) CreateSignalsHandler(w http.ResponseWriter, r *http.Request) {

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get accountID from context")
		return
	}

	// check that the user is requesting a valid signal type/sem_ver for this isn
	found := slices.Contains(claims.IsnPerms[isnSlug].SignalTypePaths, signalTypePath)
	if !found {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type", signalTypePath),
			slog.String("isn_slug", isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "signal type not available on this ISN")
		return
	}

	createSignalsRequest := CreateSignalsRequest{}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&createSignalsRequest)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	// check payload structure is valid
	if createSignalsRequest.Signals == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "request must contain a 'signals' array")
		return
	}

	if len(createSignalsRequest.Signals) == 0 {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "request must contain at least one signal in the 'signals' array")
		return
	}

	// check each signal in the payload has required fields
	for i, signal := range createSignalsRequest.Signals {
		if signal.LocalRef == "" {
			logger.ContextWithLogAttrs(r.Context(),
				slog.Int("signal_index", i),
			)

			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "signal is missing required field 'local_ref'")
			return
		}
		if len(signal.Content) == 0 {
			logger.ContextWithLogAttrs(r.Context(),
				slog.Int("signal_index", i),
				slog.String("local_ref", signal.LocalRef),
			)

			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "signal is missing required field 'content'")
			return
		}
	}

	// Ensure signal batch exists
	// The code below will automatically create batches for web users, assuming the don't already have one.
	// Note: when a new batch is created for a web user, the batch ID contained in the claims will not be updated until the next access token is issued -
	// consequently, we need continue to check the database for the latest batch while the session is open.
	// This is not ideal, but the alternative (ensuring user batches are always created in advance) is messy as it involves creating new user batches at multiple points in the code.
	//
	// Service accounts are expected to explicitly start batches.

	var signalBatchID uuid.UUID

	if claims.IsnPerms[isnSlug].SignalBatchID == nil {
		if claims.AccountType == "service_account" {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "Service accounts must create a signal batch for this ISN before posting signals")
			return
		} else {
			// create a new batch for the user if this is first time writing to the isn (the query below returns the initial batch ID for all subsequent requests in this session)
			signalBatchID, err = s.queries.CreateOrGetWebUserSignalBatch(r.Context(), database.CreateOrGetWebUserSignalBatchParams{
				Slug:      isnSlug,
				AccountID: accountID,
			})
			if err != nil {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("error", err.Error()),
					slog.String("isn_slug", isnSlug),
				)

				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
				return
			}
		}
	} else {
		signalBatchID = *claims.IsnPerms[isnSlug].SignalBatchID
	}

	createSignalsResponse := CreateSignalsResponse{
		IsnSlug:        isnSlug,
		SignalTypePath: signalTypePath,
		AccountID:      accountID,
		SignalsBatchID: signalBatchID,
		Results: CreateSignalsResults{
			StoredSignals: make([]StoredSignal, 0),
			FailedSignals: make([]FailedSignal, 0),
		},
		Summary: CreateSignalsSummary{
			TotalSubmitted: len(createSignalsRequest.Signals),
		},
	}

	// Validate all signals against schema - record validation failures
	validSignals := make([]CreateSignal, 0)
	for _, signal := range createSignalsRequest.Signals {
		err = s.schemaCache.ValidateSignal(r.Context(), s.queries, signalTypePath, signal.Content)
		if err != nil {
			// Add to failed signals list
			createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
				LocalRef:     signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeMalformedBody),
				ErrorMessage: "validation failed",
			})
		} else {
			// Add to valid signals for processing
			validSignals = append(validSignals, signal)
		}
	}

	// Process each valid signal in its own transaction
	for _, signal := range validSignals {
		// Start a new transaction for this signal
		tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			// If we can't start a transaction, record as failure and continue
			createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
				LocalRef:     signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeMalformedBody),
				ErrorMessage: "failed to begin transaction",
			})
			continue
		}

		// Create the signal master record
		var signalErr error
		var signalID uuid.UUID
		if signal.CorrelationId == nil {
			signalID, signalErr = s.queries.WithTx(tx).CreateSignal(r.Context(), database.CreateSignalParams{
				AccountID:      claims.AccountID,
				LocalRef:       signal.LocalRef,
				SignalTypeSlug: signalTypeSlug,
				SemVer:         semVer,
			})
		} else {
			// Validate that correlation_id references a valid signal in the same ISN
			isValid, err := s.queries.WithTx(tx).ValidateCorrelationID(r.Context(), database.ValidateCorrelationIDParams{
				CorrelationID: *signal.CorrelationId,
				IsnSlug:       isnSlug,
			})

			if err != nil {
				if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					logger.ContextWithLogAttrs(r.Context(),
						slog.String("error", rollbackErr.Error()),
					)

				}
				createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
					LocalRef:     signal.LocalRef,
					ErrorCode:    string(apperrors.ErrCodeInternalError),
					ErrorMessage: "Failed to validate correlation_id",
				})
				continue
			}

			if !isValid {
				if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					logger.ContextWithLogAttrs(r.Context(),
						slog.String("error", rollbackErr.Error()),
					)

				}
				createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
					LocalRef:     signal.LocalRef,
					ErrorCode:    string(apperrors.ErrCodeInvalidCorrelationID),
					ErrorMessage: fmt.Sprintf("invalid correlation_id %v - signal does not exist in this ISN", signal.CorrelationId),
				})
				continue
			}

			// create correlated signal master record
			signalID, signalErr = s.queries.WithTx(tx).CreateOrUpdateSignalWithCorrelationID(r.Context(), database.CreateOrUpdateSignalWithCorrelationIDParams{
				AccountID:      claims.AccountID,
				LocalRef:       signal.LocalRef,
				CorrelationID:  *signal.CorrelationId,
				SignalTypeSlug: signalTypeSlug,
				SemVer:         semVer,
			})
		}

		// unexpected error creating signals master record
		if signalErr != nil && !errors.Is(signalErr, pgx.ErrNoRows) {
			// Rollback this transaction
			if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				// Log the error but don't try to respond since the request may have already timed out
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("error", err.Error()),
				)

				continue
			}

			// record database errors in the failed signals array
			createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
				LocalRef:     signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeMalformedBody),
				ErrorMessage: fmt.Sprintf("database error: %v", signalErr),
			})
			continue
		}

		// Signal creation succeeded, now create the signal_version entry
		versionResult, versionErr := s.queries.WithTx(tx).CreateSignalVersion(r.Context(), database.CreateSignalVersionParams{
			AccountID:      claims.AccountID,
			SignalBatchID:  signalBatchID,
			Content:        signal.Content,
			LocalRef:       signal.LocalRef,
			SignalTypeSlug: signalTypeSlug,
			SemVer:         semVer,
		})

		if versionErr != nil {
			// Rollback this transaction
			if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				// Log the error but don't try to respond since the request may have already timed out
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("error", err.Error()),
				)

			}

			createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
				LocalRef:     signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeDatabaseError),
				ErrorMessage: fmt.Sprintf("failed to create signal version: %v", versionErr),
			})
			continue
		}

		// commit this transaction
		if err := tx.Commit(r.Context()); err != nil {
			createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
				LocalRef:     signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeDatabaseError),
				ErrorMessage: "failed to commit transaction",
			})
			continue
		}

		// Success - add to stored signals array
		createSignalsResponse.Results.StoredSignals = append(createSignalsResponse.Results.StoredSignals, StoredSignal{
			LocalRef:        signal.LocalRef,
			SignalID:        signalID,
			SignalVersionID: versionResult.ID,
			VersionNumber:   versionResult.VersionNumber,
		})
	}

	// Update summary counts
	createSignalsResponse.Summary.StoredCount = len(createSignalsResponse.Results.StoredSignals)
	createSignalsResponse.Summary.FailedCount = len(createSignalsResponse.Results.FailedSignals)

	// Log individual failures for batch tracking
	if len(createSignalsResponse.Results.FailedSignals) > 0 {
		for _, failed := range createSignalsResponse.Results.FailedSignals {
			_, err := s.queries.CreateSignalProcessingFailureDetail(r.Context(), database.CreateSignalProcessingFailureDetailParams{
				SignalBatchID:    signalBatchID,
				SignalTypeSlug:   signalTypeSlug,
				SignalTypeSemVer: semVer,
				LocalRef:         failed.LocalRef,
				ErrorCode:        failed.ErrorCode,
				ErrorMessage:     failed.ErrorMessage,
			})
			if err != nil {
				// Log the error but don't fail the operation
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("local_ref", failed.LocalRef),
					slog.String("error", err.Error()),
				)

			}
		}
	}

	var httpStatus int
	switch {
	case createSignalsResponse.Summary.StoredCount == 0:
		// All signals failed
		httpStatus = http.StatusUnprocessableEntity
	case createSignalsResponse.Summary.FailedCount > 0:
		// Partial success
		httpStatus = http.StatusMultiStatus
	default:
		// All signals processed successfully
		httpStatus = http.StatusOK
	}
	responses.RespondWithJSON(w, httpStatus, createSignalsResponse)
}

// todo sync with private
// SearchPublicSignalsHandler godocs
//
//	@Summary		Signal Search (public ISNs)
//	@Tags			Signal Exchange
//
//	@Description	Search for signals in public ISNs (no authentication required).
//	@Description
//	@Description	Note the endpoint returns the latest version of each signal.
//
//	@Param			start_date					query		string	false	"Start date"															example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"End date"																example(2006-01-02T15:15:00Z)
//	@Param			account_id					query		string	false	"Account ID"															example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Signal ID"																example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"														example(item_id_#1)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"							example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"	example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"	example(true)
//
//	@Success		200							{array}		handlers.SearchSignalResponse
//	@Failure		400							{object}	responses.ErrorResponse
//	@Failure		400							{object}	responses.ErrorResponse
//
//	@Router			/api/public/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search [get]
func (s *SignalsHandler) SearchPublicSignalsHandler(w http.ResponseWriter, r *http.Request) {

	// Parse all search parameters
	searchParams, err := parseSearchParams(r)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid search parameters")
		return
	}

	signalTypePath := fmt.Sprintf("%v/v%v", searchParams.signalTypeSlug, searchParams.semVer)

	// Validate this is a public ISN
	if !s.publicIsnCache.HasSignalType(searchParams.isnSlug, signalTypePath) {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type", signalTypePath),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "signal type not available on this ISN")
		return
	}

	// Validate search parameters
	if err := validateSearchParams(searchParams); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid search parameters")
		return
	}

	returnedSignals, err := s.queries.GetSignalsWithOptionalFilters(r.Context(), database.GetSignalsWithOptionalFiltersParams{
		IsnSlug:          searchParams.isnSlug,
		SignalTypeSlug:   searchParams.signalTypeSlug,
		SemVer:           searchParams.semVer,
		StartDate:        searchParams.startDate,
		EndDate:          searchParams.endDate,
		AccountID:        searchParams.accountID,
		SignalID:         searchParams.signalID,
		LocalRef:         searchParams.localRef,
		IncludeWithdrawn: &searchParams.includeWithdrawn,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
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
		correlatedSignalBySignalID, err = s.getCorrelatedSignals(r.Context(), signalIDs, searchParams)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
	}
	var previousVersionsBySignalID map[uuid.UUID][]PreviousSignalVersion
	if searchParams.includePreviousSignalVersions {
		previousVersionsBySignalID, err = s.getPreviousSignalVersions(r.Context(), signalIDs)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
	}

	for _, returnedSignal := range returnedSignals {
		signal := SearchSignalWithCorrelationsAndVersions{
			SearchSignal: SearchSignal{
				AccountID:            returnedSignal.AccountID,
				Email:                "", // do not show email addresses in public ISNs
				SignalID:             returnedSignal.SignalID,
				LocalRef:             returnedSignal.LocalRef,
				SignalCreatedAt:      returnedSignal.SignalCreatedAt,
				SignalVersionID:      returnedSignal.SignalVersionID,
				VersionNumber:        returnedSignal.VersionNumber,
				VersionCreatedAt:     returnedSignal.VersionCreatedAt,
				CorrelatedToSignalID: returnedSignal.CorrelatedToSignalID,
				IsWithdrawn:          returnedSignal.IsWithdrawn,
				Content:              returnedSignal.Content,
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
	responses.RespondWithJSON(w, http.StatusOK, response)
}

// SearchPrivateSignalsHandler godocs
//
//	@Summary		Signal Search (private ISNs)
//	@Tags			Signal Exchange
//
//	@Description	Search for signals by date or account in private ISNs (authentication required - only accounts with read or write permissions to the ISN can access signals).
//	@Description
//	@Description	Note the endpoint returns the latest version of each signal.
//
//	@Param			start_date					query		string	false	"Start date"															example(2006-01-02T15:05:00Z)
//	@Param			end_date					query		string	false	"End date"																example(2006-01-02T15:15:00Z)
//	@Param			account_id					query		string	false	"Account ID"															example(def87f89-dab6-4607-95f7-593d61cb5742)
//	@Param			signal_id					query		string	false	"Signal ID"																example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			local_ref					query		string	false	"Local reference"														example(item_id_#1)
//	@Param			include_withdrawn			query		string	false	"Include withdrawn signals (default: false)"							example(true)
//	@Param			include_correlated			query		string	false	"Include signals that link to each returned signal (default: false)"	example(true)
//	@Param			include_previous_versions	query		string	false	"Include previous versions of each returned signal (default: false)"	example(true)
//
//	@Success		200							{array}		handlers.SearchSignalResponse
//	@Failure		400							{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search [get]
func (s *SignalsHandler) SearchPrivateSignalsHandler(w http.ResponseWriter, r *http.Request) {

	// Parse all search parameters
	searchParams, err := parseSearchParams(r)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid search parameters")
		return
	}

	signalTypePath := fmt.Sprintf("%v/v%v", searchParams.signalTypeSlug, searchParams.semVer)

	// Validate authenticated access to private ISN
	claims, hasAuth := auth.ContextClaims(r.Context())
	if !hasAuth {
		responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "authentication required for private ISN access")
		return
	}

	// Check if user has access to this signal type
	if !slices.Contains(claims.IsnPerms[searchParams.isnSlug].SignalTypePaths, signalTypePath) {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type", signalTypePath),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "signal type not found on ISN")
		return
	}

	// Validate search parameters
	if err := validateSearchParams(searchParams); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid search parameters")
		return
	}

	returnedSignals, err := s.queries.GetSignalsWithOptionalFilters(r.Context(), database.GetSignalsWithOptionalFiltersParams{
		IsnSlug:          searchParams.isnSlug,
		SignalTypeSlug:   searchParams.signalTypeSlug,
		SemVer:           searchParams.semVer,
		StartDate:        searchParams.startDate,
		EndDate:          searchParams.endDate,
		AccountID:        searchParams.accountID,
		SignalID:         searchParams.signalID,
		LocalRef:         searchParams.localRef,
		IncludeWithdrawn: &searchParams.includeWithdrawn,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_slug", searchParams.isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
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
		correlatedSignalBySignalID, err = s.getCorrelatedSignals(r.Context(), signalIDs, searchParams)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
	}
	var previousVersionsBySignalID map[uuid.UUID][]PreviousSignalVersion
	if searchParams.includePreviousSignalVersions {
		previousVersionsBySignalID, err = s.getPreviousSignalVersions(r.Context(), signalIDs)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
	}

	for _, returnedSignal := range returnedSignals {
		signal := SearchSignalWithCorrelationsAndVersions{
			SearchSignal: SearchSignal{
				AccountID:            returnedSignal.AccountID,
				Email:                returnedSignal.Email,
				SignalID:             returnedSignal.SignalID,
				LocalRef:             returnedSignal.LocalRef,
				SignalCreatedAt:      returnedSignal.SignalCreatedAt,
				SignalVersionID:      returnedSignal.SignalVersionID,
				VersionNumber:        returnedSignal.VersionNumber,
				VersionCreatedAt:     returnedSignal.VersionCreatedAt,
				CorrelatedToSignalID: returnedSignal.CorrelatedToSignalID,
				IsWithdrawn:          returnedSignal.IsWithdrawn,
				Content:              returnedSignal.Content,
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
	responses.RespondWithJSON(w, http.StatusOK, response)
}

// WithdrawSignalHandler godoc
//
//	@Summary		Withdraw a signal
//	@Description	Withdraw a signal by local reference
//	@Description
//	@Description	Withdrawn signals are hidden from search results by default but remain in the database.
//	@Description	Signals can only be withdrawn by the account that created the signal.
//	@Description	To reactivate a signal resupply it with the same local_ref using the 'create signals' end point.
//
//	@Tags			Signal Exchange
//
//	@Param			isn_slug			path	string							true	"ISN slug"				example(sample-isn--example-org)
//	@Param			signal_type_slug	path	string							true	"Signal type slug"		example(signal-type-1)
//	@Param			sem_ver				path	string							true	"Signal type version"	example(0.0.1)
//	@Param			request				body	handlers.WithdrawSignalRequest	true	"Withdrawal request"
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/withdraw [put]
func (s *SignalsHandler) WithdrawSignalHandler(w http.ResponseWriter, r *http.Request) {

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get accountID from context")
		return
	}

	// check that the user is requesting a valid signal type/sem_ver for this isn
	found := slices.Contains(claims.IsnPerms[isnSlug].SignalTypePaths, signalTypePath)
	if !found {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type", signalTypePath),
			slog.String("isn_slug", isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceNotFound, "signal type not available on this ISN")
		return
	}

	// Parse request body
	var req WithdrawSignalRequest
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	if req.LocalRef == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply a local_ref")
		return
	}

	// Get the signal
	signal, err := s.queries.GetSignalByAccountAndLocalRef(r.Context(), database.GetSignalByAccountAndLocalRefParams{
		AccountID: accountID,
		Slug:      signalTypeSlug,
		SemVer:    semVer,
		LocalRef:  *req.LocalRef,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "signal not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("local_ref", *req.LocalRef),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Check if signal is already withdrawn
	if signal.IsWithdrawn {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceAlreadyExists, "signal is already withdrawn")
		return
	}

	// Withdraw the signal
	rowsAffected, err := s.queries.WithdrawSignalByLocalRef(r.Context(), database.WithdrawSignalByLocalRefParams{
		AccountID: accountID,
		Slug:      signalTypeSlug,
		SemVer:    semVer,
		LocalRef:  *req.LocalRef,
	})

	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("local_ref", *req.LocalRef),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if rowsAffected == 0 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "attempt to withdraw signal failed - no rows updated")
		return
	}

	logger.ContextWithLogAttrs(r.Context(),
		slog.String("signal_id", signal.ID.String()),
		slog.String("local_ref", signal.LocalRef),
		slog.String("withdrawn_by", accountID.String()),
	)

	responses.RespondWithStatusCodeOnly(w, http.StatusNoContent)
}
