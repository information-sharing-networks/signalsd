package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"slices"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/isns"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
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

// request json
type Signal struct {
	LocalRef      string          `json:"local_ref" example:"item_id_#1"`
	CorrelationId *uuid.UUID      `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"` //optional - supply the id of another signal if you want to link to it
	Content       json.RawMessage `json:"content"`
}

type CreateSignalsRequest struct {
	Signals []Signal `json:"signals"`
}

// workaround this struct is only needed for Swaggo documentation (swaggo does not understand json.RawMessage type)
type CreateSignalsRequestDoc struct {
	Signals []struct {
		LocalRef      string         `json:"local_ref" example:"item_id_#1"`
		CorrelationId *uuid.UUID     `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"`
		Content       map[string]any `json:"content"`
	} `json:"signals"`
}

type CreateSignalsResponse struct {
	IsnSlug        string               `json:"isn_slug" example:"sample-isn--example-org"`
	SignalTypePath string               `json:"signal_type_path" example:"signal-type-1/v0.0.1"`
	AccountID      uuid.UUID            `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	SignalsBatchID uuid.UUID            `json:"signals_batch_id" example:"b51faf05-aaed-4250-b334-2258ccdf1ff2"`
	Results        CreateSignalsResults `json:"results"`
	Summary        CreateSignalsSummary `json:"summary"`
}

// partial loads are possible - this struct tracks failures
type CreateSignalsResults struct {
	StoredSignals []StoredSignal `json:"stored_signals"`
	FailedSignals []FailedSignal `json:"failed_signals,omitempty"`
}

type StoredSignal struct {
	LocalRef        string    `json:"local_ref" example:"item_id_#1"`
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

// structs used by the search endpoint
type SearchParams struct {
	IsnSlug          string
	SignalTypeSlug   string
	SemVer           string
	AccountID        *uuid.UUID
	StartDate        *time.Time
	EndDate          *time.Time
	IncludeWithdrawn bool
}

type WithdrawSignalRequest struct {
	LocalRef *string `json:"local_ref,omitempty" example:"item_id_#1"`
}

type SignalVersion struct {
	AccountID          uuid.UUID       `json:"account_id"`
	AccountType        string          `json:"account_type"`
	Email              string          `json:"email,omitempty"`
	LocalRef           string          `json:"local_ref"`
	VersionNumber      int32           `json:"version_number"`
	CreatedAt          time.Time       `json:"created_at"`
	SignalVersionID    uuid.UUID       `json:"signal_version_id"`
	SignalID           uuid.UUID       `json:"signal_id"`
	CorrelatedLocalRef string          `json:"correlated_local_ref"`
	CorrelatedSignalID uuid.UUID       `json:"correlated_signal_id"`
	IsWithdrawn        bool            `json:"is_withdrawn"`
	Content            json.RawMessage `json:"content"`
}
type SignalVersionDoc struct {
	AccountID          uuid.UUID      `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	AccountType        string         `json:"account_type" example:"user"`
	Email              string         `json:"email" example:"user@example.com"`
	LocalRef           string         `json:"local_ref" example:"item_id_#1"`
	VersionNumber      int32          `json:"version_number" example:"1"`
	CreatedAt          time.Time      `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	SignalVersionID    uuid.UUID      `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`
	SignalID           uuid.UUID      `json:"signal_id" example:"4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19"`
	CorrelatedLocalRef string         `json:"correlated_local_ref" example:"item_id_#2"`
	CorrelatedSignalID uuid.UUID      `json:"correlated_signal_id" example:"17c50d26-1da6-4ac0-897f-3a2f85f07cd3"`
	IsWithdrawn        bool           `json:"is_withdrawn" example:"false"`
	Content            map[string]any `json:"content"`
}

// SignalsHandler godocs
//
//	@Summary		Create signals
//	@Tags			Signal sharing
//
//	@Description	Submit an array of signals for storage on the ISN
//	@Description	- payloads must not mix signals of different types and are subject to the size limits defined on the site.
//	@Description	- The client-supplied local_ref must uniquely identify each signal of the specified signal type that will be supplied by the account.
//	@Description	- If a local reference is received more than once from an account for the specified signal_type it will be stored with a incremented version number.
//	@Description	- Optionally a correlation_id can be supplied - this will link the signal to a previously received signal. The correlated signal does not need to be owned by the same account but must be in the same ISN.
//	@Description	- requests are only accepted for the open signal batch for this account/ISN.
//	@Description
//	@Description	**Authentication**
//	@Description
//	@Description	Requires a valid access token.
//	@Description	The claims in the access token list the ISNs and signal_types that the account is permitted to use.
//	@Description
//	@Description	**Validation and Processing**
//	@Description
//	@Description	Signals are validated against the JSON schema specified for the signal type unless validation is disabled on the type definition.
//	@Description	Individual signal processing failures (validation errors, incorrect correlation ids, database errors) are recorded in the response but do not prevent other signals from being processed.
//	@Description
//	@Description	When validation is disabled, basic checks are still done on the incoming data and the following issues create a 400 error and cause the entire payload to be rejected:
//	@Description	- invalid json format
//	@Description	- missing fields (the array of signals must be in a json object called signals, and content and local_ref must be present for each record).
//	@Description
//	@Description
//	@Description	**Response Status Codes**
//	@Description	- 200: All signals processed successfully
//	@Description	- 207: Partial success (some signals succeeded, some failed)
//	@Description	- 422: All signals failed processing (the request was valid but all signals failed processing, e.g because they did not validate against the schema)
//	@Description	- 400 / error_code = 'malformed_body': Invalid request format, authentication, or other request-level errors
//	@Description	- 400: authentication, or other request-level errors
//	@Description	- 500: Internal server errors
//	@Description
//	@Description	400 and 500 errors cause the whole payload to be rejected (note these failures are not logged on the database and should be handled by the client immediately).
//
//	@Param			isn_slug			path		string								true	"isn slug"						example(sample-isn--example-org)
//	@Param			signal_type_slug	path		string								true	"signal type slug"				example(sample-signal--example-org)
//	@Param			sem_ver				path		string								true	"signal type sem_ver number"	example(0.0.1)
//	@Param			request				body		handlers.CreateSignalsRequestDoc	true	"create signals"
//
//	@Success		200					{object}	handlers.CreateSignalsResponse		"All signals processed successfully"
//	@Success		207					{object}	handlers.CreateSignalsResponse		"Partial success - some signals succeeded, some failed"
//	@Success		422					{object}	handlers.CreateSignalsResponse		"All signals failed processing - returns detailed error information"
//	@Failure		400					{object}	responses.ErrorResponse				"Invalid request format or authentication failure"
//	@Failure		500					{object}	responses.ErrorResponse				"Internal server error"
//
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals [post]
//
// CreateSignal Handler inserts signals and signal_versions records - signals are the master records containing
// the local_ref and correlation_id, signal_versions contains the content and links back to the signal.  Multiple versions are created when signals are resupplied, e.g due to corrections being sent.
//
// the handler will record errors encountered when processing individual signals (see the signal_processing_failures table).
// Errors that relate to the entire request - e.g invalid json, permissions and authentication - are not recorded and should be handled by the client when they occur.
func (s *SignalsHandler) CreateSignalsHandler(w http.ResponseWriter, r *http.Request) {

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	claims, ok := auth.ContextAccessTokenClaims(r.Context())
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
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("signal type %v is not available on ISN %v", signalTypePath, isnSlug))
		return
	}

	createSignalsRequest := CreateSignalsRequest{}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&createSignalsRequest)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid JSON format: %v", err))
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
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("signal at index %d is missing required field 'local_ref'", i))
			return
		}
		if len(signal.Content) == 0 {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("signal at index %d is missing required field 'content'", i))
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
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to get or create signal batch: %v", err))
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
	validSignals := make([]Signal, 0)
	for _, signal := range createSignalsRequest.Signals {
		err = s.schemaCache.ValidateSignal(r.Context(), s.queries, signalTypePath, signal.Content)
		if err != nil {
			// Add to failed signals list
			createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
				LocalRef:     signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeMalformedBody),
				ErrorMessage: fmt.Sprintf("validation failed: %v", err),
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
				ErrorMessage: fmt.Sprintf("failed to begin transaction: %v", err),
			})
			continue
		}

		// Create the signal
		var signalErr error
		if signal.CorrelationId == nil {
			_, signalErr = s.queries.WithTx(tx).CreateSignal(r.Context(), database.CreateSignalParams{
				AccountID:      claims.AccountID,
				LocalRef:       signal.LocalRef,
				SignalTypeSlug: signalTypeSlug,
				SemVer:         semVer,
			})
		} else {
			// Validate that correlation_id references a valid signal in the same ISN
			// note there is no longer a db constraint to protect against errors here (the signals table is now partitioned by author and this prevents the use of a foreign key since signals can be correlated to signals from any account)
			isValid, err := s.queries.WithTx(tx).ValidateCorrelationID(r.Context(), database.ValidateCorrelationIDParams{
				CorrelationID: *signal.CorrelationId,
				IsnSlug:       isnSlug,
			})

			if err != nil {
				createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
					LocalRef:     signal.LocalRef,
					ErrorCode:    string(apperrors.ErrCodeInternalError),
					ErrorMessage: "Failed to validate correlation_id",
				})
				continue
			}

			if !isValid {
				createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
					LocalRef:     signal.LocalRef,
					ErrorCode:    string(apperrors.ErrCodeMalformedBody),
					ErrorMessage: fmt.Sprintf("invalid correlation_id %v - signal does not exist in this ISN", signal.CorrelationId),
				})
				continue
			}

			_, signalErr = s.queries.WithTx(tx).CreateOrUpdateSignalWithCorrelationID(r.Context(), database.CreateOrUpdateSignalWithCorrelationIDParams{
				AccountID:      claims.AccountID,
				LocalRef:       signal.LocalRef,
				CorrelationID:  *signal.CorrelationId,
				SignalTypeSlug: signalTypeSlug,
				SemVer:         semVer,
			})
		}

		if signalErr != nil && !errors.Is(signalErr, pgx.ErrNoRows) {
			// Rollback this transaction
			if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				// Log the error but don't try to respond since the request may have already timed out
				logger := zerolog.Ctx(r.Context())
				logger.Error().Err(err).Msg("failed to rollback transaction")
				continue
			}

			// Handle other database errors
			createSignalsResponse.Results.FailedSignals = append(createSignalsResponse.Results.FailedSignals, FailedSignal{
				LocalRef:     signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeMalformedBody),
				ErrorMessage: fmt.Sprintf("database error: %v", signalErr),
			})
			continue
		}

		// Signal creation succeeded, now try to create the signal_version entry
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
				logger := zerolog.Ctx(r.Context())
				logger.Error().Err(err).Msg("failed to rollback transaction")
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
				ErrorMessage: fmt.Sprintf("failed to commit transaction: %v", err),
			})
			continue
		}

		// Success - add to stored signals
		createSignalsResponse.Results.StoredSignals = append(createSignalsResponse.Results.StoredSignals, StoredSignal{
			LocalRef:        signal.LocalRef,
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
				SignalBatchID:    *claims.IsnPerms[isnSlug].SignalBatchID,
				SignalTypeSlug:   signalTypeSlug,
				SignalTypeSemVer: semVer,
				LocalRef:         failed.LocalRef,
				ErrorCode:        failed.ErrorCode,
				ErrorMessage:     failed.ErrorMessage,
			})
			if err != nil {
				// Log the error but don't fail the operation
				logger := zerolog.Ctx(r.Context())
				logger.Error().Msgf("failed to log signal processing failure for local_ref %s: %v", failed.LocalRef, err)
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

// SearchPublicSignalsHandler godocs
//
//	@Summary		Search for signals in public ISNs
//	@Tags			Signal sharing
//
//	@Description	Search for signals by date or account in public ISNs (no authentication required)
//	@Description
//	@Description	Note the endpoint returns the latest version of each signal and does not include withdrawn or archived signals
//
//	@Param			start_date	query		string	false	"Start date for filtering"	example(2006-01-02T15:04:05Z)
//	@Param			end_date	query		string	false	"End date for filtering"	example(2006-01-02T16:00:00Z
//	@Param			account_id	query		string	false	"Account ID for filtering"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		200			{array}		handlers.SignalVersionDoc
//	@Failure		400			{object}	responses.ErrorResponse
//
//	@Router			/api/public/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search [get]
func (s *SignalsHandler) SearchPublicSignalsHandler(w http.ResponseWriter, r *http.Request) {
	// set up params
	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	searchParams := SearchParams{
		IsnSlug:          isnSlug,
		SignalTypeSlug:   signalTypeSlug,
		SemVer:           semVer,
		IncludeWithdrawn: false, // Default to false for public endpoints
	}

	if accountIDString := r.URL.Query().Get("account_id"); accountIDString != "" {
		accountID, err := uuid.Parse(accountIDString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "account_id is not a valid UUID")
			return
		}
		searchParams.AccountID = &accountID
	}

	if startDateString := r.URL.Query().Get("start_date"); startDateString != "" {
		startDate, err := utils.ParseDateTime(startDateString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.StartDate = &startDate
	}

	if endDateString := r.URL.Query().Get("end_date"); endDateString != "" {
		endDate, err := utils.ParseDateTime(endDateString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.EndDate = &endDate
	}

	// Validate this is a public ISN
	if !s.publicIsnCache.HasSignalType(isnSlug, signalTypePath) {
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("invalid public ISN or signal type %v is not available on this ISN", signalTypePath))
		return
	}

	hasPartialDateRange := (searchParams.StartDate != nil) != (searchParams.EndDate != nil)
	hasDateRange := searchParams.StartDate != nil && searchParams.EndDate != nil
	hasAccount := searchParams.AccountID != nil

	if hasPartialDateRange {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "you must supply both start_date and end_date search parameters")
		return
	}
	if !hasDateRange && !hasAccount {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "you must supply a search paramenter")
		return
	}

	rows, err := s.queries.GetLatestSignalVersionsWithOptionalFilters(r.Context(), database.GetLatestSignalVersionsWithOptionalFiltersParams{
		IsnSlug:          searchParams.IsnSlug,
		SignalTypeSlug:   searchParams.SignalTypeSlug,
		SemVer:           searchParams.SemVer,
		StartDate:        searchParams.StartDate,
		EndDate:          searchParams.EndDate,
		AccountID:        searchParams.AccountID,
		IncludeWithdrawn: &searchParams.IncludeWithdrawn,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to select signals from database: %v", err))
		return
	}

	res := make([]SignalVersion, 0, len(rows))
	for _, row := range rows {
		// Always hide email addresses for public ISN responses for privacy
		res = append(res, SignalVersion{
			AccountID:          row.AccountID,
			AccountType:        row.AccountType,
			Email:              "", // hidden for public ISNs
			LocalRef:           row.LocalRef,
			VersionNumber:      row.VersionNumber,
			CreatedAt:          row.CreatedAt,
			SignalVersionID:    row.SignalVersionID,
			SignalID:           row.SignalID,
			CorrelatedLocalRef: row.CorrelatedLocalRef,
			CorrelatedSignalID: row.CorrelatedSignalID,
			IsWithdrawn:        row.IsWithdrawn,
			Content:            row.Content,
		})
	}
	responses.RespondWithJSON(w, http.StatusOK, res)
}

// SearchPrivateSignalsHandler godocs
//
//	@Summary		Search for signals in private ISNs
//	@Tags			Signal sharing
//
//	@Description	Search for signals by date or account in private ISNs (authentication required - only accounts with read or write permissions to the ISN can access signals)
//	@Description
//	@Description	Note the endpoint returns the latest version of each signal and does not include withdrawn or archived signals
//
//	@Param			start_date			query		string	false	"Start date for filtering"						example(2006-01-02T15:04:05Z)
//	@Param			end_date			query		string	false	"End date for filtering"						example(2006-01-02T16:00:00Z
//	@Param			account_id			query		string	false	"Account ID for filtering"						example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//	@Param			include_withdrawn	query		string	false	"Include withdrawn signals (default: false)"	example(false)
//
//	@Success		200					{array}		handlers.SignalVersionDoc
//	@Failure		400					{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search [get]
func (s *SignalsHandler) SearchPrivateSignalsHandler(w http.ResponseWriter, r *http.Request) {
	// set up params
	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	searchParams := SearchParams{
		IsnSlug:          isnSlug,
		SignalTypeSlug:   signalTypeSlug,
		SemVer:           semVer,
		IncludeWithdrawn: false, // Default to false
	}

	if accountIDString := r.URL.Query().Get("account_id"); accountIDString != "" {
		accountID, err := uuid.Parse(accountIDString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "account_id is not a valid UUID")
			return
		}
		searchParams.AccountID = &accountID
	}

	if startDateString := r.URL.Query().Get("start_date"); startDateString != "" {
		startDate, err := utils.ParseDateTime(startDateString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.StartDate = &startDate
	}

	if endDateString := r.URL.Query().Get("end_date"); endDateString != "" {
		endDate, err := utils.ParseDateTime(endDateString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.EndDate = &endDate
	}

	if includeWithdrawnString := r.URL.Query().Get("include_withdrawn"); includeWithdrawnString != "" {
		includeWithdrawn := includeWithdrawnString == "true"
		searchParams.IncludeWithdrawn = includeWithdrawn
	}

	// Validate authenticated access to private ISN
	claims, hasAuth := auth.ContextAccessTokenClaims(r.Context())
	if !hasAuth {
		responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "authentication required for private ISN access")
		return
	}

	// Check if user has access to this signal type
	if !slices.Contains(claims.IsnPerms[isnSlug].SignalTypePaths, signalTypePath) {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, fmt.Sprintf("access denied to signal type %v on ISN %v", signalTypePath, isnSlug))
		return
	}

	hasPartialDateRange := (searchParams.StartDate != nil) != (searchParams.EndDate != nil)
	hasDateRange := searchParams.StartDate != nil && searchParams.EndDate != nil
	hasAccount := searchParams.AccountID != nil

	if hasPartialDateRange {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "you must supply both start_date and end_date search parameters")
		return
	}
	if !hasDateRange && !hasAccount {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "you must supply a search paramenter")
		return
	}

	rows, err := s.queries.GetLatestSignalVersionsWithOptionalFilters(r.Context(), database.GetLatestSignalVersionsWithOptionalFiltersParams{
		IsnSlug:          searchParams.IsnSlug,
		SignalTypeSlug:   searchParams.SignalTypeSlug,
		SemVer:           searchParams.SemVer,
		StartDate:        searchParams.StartDate,
		EndDate:          searchParams.EndDate,
		AccountID:        searchParams.AccountID,
		IncludeWithdrawn: &searchParams.IncludeWithdrawn,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to select signals from database: %v", err))
		return
	}

	res := make([]SignalVersion, 0, len(rows))
	for _, row := range rows {
		// Include full information including email addresses for authenticated users
		res = append(res, SignalVersion{
			AccountID:          row.AccountID,
			AccountType:        row.AccountType,
			Email:              row.Email,
			LocalRef:           row.LocalRef,
			VersionNumber:      row.VersionNumber,
			CreatedAt:          row.CreatedAt,
			SignalVersionID:    row.SignalVersionID,
			SignalID:           row.SignalID,
			CorrelatedLocalRef: row.CorrelatedLocalRef,
			CorrelatedSignalID: row.CorrelatedSignalID,
			IsWithdrawn:        row.IsWithdrawn,
			Content:            row.Content,
		})
	}
	responses.RespondWithJSON(w, http.StatusOK, res)
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
//	@Tags			Signal sharing
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

	claims, ok := auth.ContextAccessTokenClaims(r.Context())
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
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("signal type %v is not available on ISN %v", signalTypePath, isnSlug))
		return
	}

	// Parse request body
	var req WithdrawSignalRequest
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
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
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
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
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to withdraw signal: %v", err))
		return
	}

	if rowsAffected == 0 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "attempt to withdraw signal failed - no rows updated")
		return
	}

	logger := zerolog.Ctx(r.Context())
	logger.Info().Msgf("Signal %v (local ref: %v) withdrawn by %v", signal.ID, signal.LocalRef, accountID)

	responses.RespondWithStatusCodeOnly(w, http.StatusNoContent)
}
