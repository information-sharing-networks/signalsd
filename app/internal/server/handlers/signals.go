package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"slices"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SignalsHandler struct {
	queries *database.Queries
	pool    *pgxpool.Pool
}

func NewSignalsHandler(queries *database.Queries, pool *pgxpool.Pool) *SignalsHandler {
	return &SignalsHandler{
		queries: queries,
		pool:    pool,
	}
}

type CreateSignalsRequest struct {
	Signals []struct {
		LocalRef      string          `json:"local_ref" example:"item_id_#1"`
		CorrelationId *uuid.UUID      `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"` //optional - supply the id of another signal if you want to link to it
		Content       json.RawMessage `json:"content"`
	} `json:"signals"`
}

// workaround this structure is only needed for Swaggo documentation (swaggo does not understand json.RawMessage type)
type CreateSignalsRequestDoc struct {
	Signals []struct {
		LocalRef      string         `json:"local_ref" example:"item_id_#1"`
		CorrelationId *uuid.UUID     `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"`
		Content       map[string]any `json:"content"`
	} `json:"signals"`
}

type CreateSignalsResponse struct {
	IsnSlug        string         `json:"isn_slug" example:"sample-isn--example-org"`
	SignalTypePath string         `json:"signal_type_path" example:"signal-type-1/v0.0.1"`
	AccountID      uuid.UUID      `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	StoredSignals  []StoredSignal `json:"stored_signals"`
}
type StoredSignal struct {
	LocalRef        string    `json:"local_ref" example:"item_id_#1"`
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`
	VersionNumber   int32     `json:"version_number" example:"1"`
}

type SearchParams struct {
	IsnSlug        string     `json:"isn_id"`
	SignalTypeSlug string     `json:"signal_type_id"`
	SemVer         string     `json:"sem_ver"`
	AccountID      *uuid.UUID `json:"account_id,omitempty"`
	StartDate      *time.Time `json:"start_date,omitempty"`
	EndDate        *time.Time `json:"end_date,omitempty"`
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
	Content            map[string]any `json:"content"`
}

// SignalsHandler godocs
//
//	@Summary		Send signals
//	@Tags			Signal sharing
//
//	@Description	- the client can submit an array of signals to this endpoint for storage on the ISN
//	@Description	- payloads must not mix signals of different types and the payload is subject to the sizen	limits defined on the ISN.
//	@Description	- The client-supplied local_ref must uniquely identify each signal of the specified signal type that will be supplied by the account.
//	@Description	- If a local reference is received more than once from an account for a specified signal_type it will be stored with a incremented version number.
//	@Description	- Optionally a correlation_id can be supplied - this will link the signal to a previously received signal. The correlated signal does not need to be owned by the same account.
//	@Description	- requests are only accepted for the open signal batch for this account/ISN.
//	@Description
//	@Description	**Authentication**
//	@Description
//	@Description	Requires a valid access token.
//	@Description	The claims in the access token list the ISNs and signal_types that the account is permitted to use.
//	@Description
//	@Description	- the RequireIsnPermission middleware will consult the claims in the access token to confirm the user is allowed to write to the isn in the URL.
//	@Description	- This handler also checks that the signal_type/sem_ver in the url is also listed in the claims (this is to catch mistyped urls)
//	@Description
//	@Description	**Validation**
//	@Description
//	@Description	Signals are validated against the json schema specified for the signal type unless validation is disabled on the type definition. The entire payload is rejected with a 400 error if validation fails.
//	@Description
//	@Description	When validation is disabled, basic checks are still done on the incoming data. The following issues create a 400 error and cause the entire payload to be rejected:
//	@Description	- invalid json format
//	@Description	- missing fields (the array of signals must be in a json object called signals, and content and local_ref must be present for each record).
//	@Description	- incorrect correlation ids - where supplied, correlation ids must refer to another signal ID in the ISN (error_code is set to "invalid_correlation_id" in this is not the case)
//	@Description
//	@Description	internal errors cause the whole payload to be rejected.
//
//	@Param			isn_slug			path		string								true	"isn slug"						example(sample-isn--example-org)
//	@Param			signal_type_slug	path		string								true	"signal type slug"				example(sample-signal--example-org)
//	@Param			sem_ver				path		string								true	"signal type sem_ver number"	example(0.0.1)
//	@Param			request				body		handlers.CreateSignalsRequestDoc	true	"create signals"
//
//	@Success		201					{object}	handlers.CreateSignalsResponse
//	@Failure		400					{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals [post]
//
// CreateSignal Handler inserts signals and signal_versions records - signals are the master records containing
// the local_ref and correlation_id, signal_versions contains the content and links back to the signal.  Multiple versions are created when signals are resupplied, e.g due to corrections being sent.
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

	// check the account has an open batch for this isn
	if claims.IsnPerms[isnSlug].SignalBatchID == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("you must open a batch for ISN %v before sending signals", isnSlug))
		return
	}

	// check that this the user is requesting a valid signal type/sem_ver for this isn
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

	createSignalsResponse := CreateSignalsResponse{
		IsnSlug:        isnSlug,
		SignalTypePath: signalTypePath,
		AccountID:      accountID,
	}

	storedSignals := make([]StoredSignal, 0)

	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to begin transaction: %v", err))
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			// Log the error but don't try to respond since the request may have already timed out
			fmt.Printf("failed to rollback transaction: %v\n", err)
		}
	}()

	// Validate all signals first (before any database operations)
	for i, signal := range createSignalsRequest.Signals {
		// Validate signal content against schema (includes JSON syntax validation)
		err = schemas.ValidateSignal(r.Context(), s.queries, signalTypePath, signal.Content)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody,
				fmt.Sprintf("signal %d (local_ref: %s) failed validation: %v", i+1, signal.LocalRef, err))
			return
		}
	}

	// sql statements are batched to reduced the number of round trips to the database
	signalBatch := &pgx.Batch{}
	versionBatch := &pgx.Batch{}

	// prepare signal creation batch
	for _, signal := range createSignalsRequest.Signals {
		if signal.CorrelationId == nil {
			signalBatch.Queue(database.CreateSignal,
				claims.AccountID, signal.LocalRef, signalTypeSlug, semVer)
		} else {
			signalBatch.Queue(database.CreateOrUpdateSignalWithCorrelationID,
				claims.AccountID, signal.LocalRef, *signal.CorrelationId, signalTypeSlug, semVer)
		}
	}

	// Execute signal creation batch
	signalResults := tx.SendBatch(r.Context(), signalBatch)

	// Check signal creation results for errors (ignore ErrNoRows for existing signals)
	var signalProcessingError error
	for i, signal := range createSignalsRequest.Signals {
		var signalID uuid.UUID
		err := signalResults.QueryRow().Scan(&signalID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				if pgErr.Code == "23503" && pgErr.ConstraintName == "fk_correlation_id" {
					signalProcessingError = fmt.Errorf("signal %d (local_ref: %s) has invalid correlation_id %v", i+1, signal.LocalRef, signal.CorrelationId)
					break
				}
			}
			signalProcessingError = fmt.Errorf("database error processing signal %d (local_ref: %s): %v", i+1, signal.LocalRef, err)
			break
		}
	}

	// Close batch results before checking for errors to prevent "conn busy"
	closeErr := signalResults.Close()
	if closeErr != nil {
		fmt.Printf("Error closing signal batch results: %v\n", closeErr)
	}

	// Now handle any processing errors
	if signalProcessingError != nil {
		if strings.Contains(signalProcessingError.Error(), "invalid correlation_id") {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidCorrelationID, signalProcessingError.Error())
		} else {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, signalProcessingError.Error())
		}
		return
	}

	// Prepare version creation batch
	for _, signal := range createSignalsRequest.Signals {
		versionBatch.Queue(database.CreateSignalVersion,
			claims.AccountID,
			*claims.IsnPerms[isnSlug].SignalBatchID,
			signal.Content,
			signal.LocalRef,
			signalTypeSlug,
			semVer,
		)
	}

	// Execute version creation batch
	versionResults := tx.SendBatch(r.Context(), versionBatch)

	// Process version creation results
	var versionProcessingError error
	for i, signal := range createSignalsRequest.Signals {
		var versionID uuid.UUID
		var versionNumber int32
		err := versionResults.QueryRow().Scan(&versionID, &versionNumber)
		if err != nil {
			versionProcessingError = fmt.Errorf("database error creating version for signal %d (local_ref: %s): %v", i+1, signal.LocalRef, err)
			break
		}

		storedSignals = append(storedSignals, StoredSignal{
			LocalRef:        signal.LocalRef,
			SignalVersionID: versionID,
			VersionNumber:   versionNumber,
		})
	}

	// CRITICAL: Close batch results before checking for errors to prevent "conn busy"
	closeErr = versionResults.Close()
	if closeErr != nil {
		fmt.Printf("Error closing version batch results: %v\n", closeErr)
	}

	// Now handle any processing errors
	if versionProcessingError != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, versionProcessingError.Error())
		return
	}

	createSignalsResponse.StoredSignals = storedSignals

	if err = tx.Commit(r.Context()); err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}
	// for array insert
	responses.RespondWithJSON(w, http.StatusCreated, createSignalsResponse)
}

// DeleteSignalsHandler godocs
//
//	@Summary	Withdraw a signal (TODO)
//	@Tags		Signal sharing
//
//	@Failure	501	{object}	responses.ErrorResponse	"Not implemented"
//
//	@Router		/isn/{isn_slug}/signal_types/{signal_type_slug}/{signal_id} [delete]
func (s *SignalsHandler) DeleteSignalHandler(w http.ResponseWriter, r *http.Request) {
	// mark as withdrawn
	responses.RespondWithError(w, r, http.StatusNotImplemented, apperrors.ErrCodeNotImplemented, "todo - delete signal not yet implemented")
}

// GetSignalHandler godocs
//
//	@Summary	get a signal (TODO)
//	@Tags		Signal sharing
//
//	@Failure	501	{object}	responses.ErrorResponse	"Not implemented"
//
//	@Router		/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/{signal_id} [get]
func (s *SignalsHandler) GetSignalHandler(w http.ResponseWriter, r *http.Request) {
	// todo option to
	// option to search by local ref
	// reutrn history of versions
	// return withdrawn
	responses.RespondWithError(w, r, http.StatusNotImplemented, apperrors.ErrCodeNotImplemented, "todo - iget signal not yet implemented")
}

// GetSignalHandler godocs
//
//	@Summary		Search for signals
//	@Tags			Signal sharing
//
//	@Description	Search for signals by date or account
//	@Description
//	@Description	Accepted timestamps formats (ISO 8601):
//	@Description	- 2006-01-02T15:04:05Z (UTC)
//	@Description	- 2006-01-02T15:04:05+07:00 (with offset)
//	@Description	- 2006-01-02T15:04:05.999999999Z (nano precision)
//	@Description
//	@Description	Note: If the timestamp contains a timezone offset (as in +07:00), the + must be percent-encoded as %2B in the query strings.
//	@Description
//	@Description	Dates (YYYY-MM-DD) can also be used.
//	@Description	These are treated as the start of day UTC (so 2006-01-02 is treated as 2006-01-02T00:00:00Z)
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
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search [get]
func (s *SignalsHandler) SearchSignalsHandler(w http.ResponseWriter, r *http.Request) {

	// set up params
	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	searchParams := SearchParams{
		IsnSlug:        isnSlug,
		SignalTypeSlug: signalTypeSlug,
		SemVer:         semVer,
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

	// check that this the user is requesting a valid signal type/sem_ver for this isn
	claims, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	found := slices.Contains(claims.IsnPerms[isnSlug].SignalTypePaths, signalTypePath)
	if !found {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("signal type %v is not available on ISN %v", signalTypePath, isnSlug))
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
		IsnSlug:        searchParams.IsnSlug,
		SignalTypeSlug: searchParams.SignalTypeSlug,
		SemVer:         searchParams.SemVer,
		StartDate:      searchParams.StartDate,
		EndDate:        searchParams.EndDate,
		AccountID:      searchParams.AccountID,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to select signals from database: %v", err))
		return
	}

	res := make([]SignalVersion, 0, len(rows))
	for _, row := range rows {
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
			Content:            row.Content,
		})
	}
	// check for valid ISO dates
	responses.RespondWithJSON(w, http.StatusOK, res)
}
