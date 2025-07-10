package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
)

type SignalsBatchHandler struct {
	queries *database.Queries
}

func NewSignalsBatchHandler(queries *database.Queries) *SignalsBatchHandler {
	return &SignalsBatchHandler{queries: queries}
}

type CreateSignalsBatchRequest struct {
	IsnSlug string `json:"isn_slug" example:"sample-isn--example-org"`
}

type CreateSignalsBatchResponse struct {
	ResourceURL    string    `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org/account/{account_id}/batch/{signals_batch_id}"`
	AccountID      uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	SignalsBatchID uuid.UUID `json:"signals_batch_id" example:"b51faf05-aaed-4250-b334-2258ccdf1ff2"`
}

// FailureRow represents a failed local_ref for a specific signal type
type FailureRow struct {
	LocalRef     string `json:"local_ref"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// BatchStatus represents the summary status for all signal types in a batch
type BatchStatus struct {
	SignalTypeSlug     string       `json:"signal_type_slug"`
	SignalTypeVersion  string       `json:"signal_type_version"`
	StoredCount        int64        `json:"stored_count"`
	FailedCount        int64        `json:"failed_count"`
	UnresolvedFailures []FailureRow `json:"unresolved_failures,omitempty"`
}

// BatchStatusResponse represents the complete batch status with successful loads and failures
type BatchStatusResponse struct {
	BatchID          uuid.UUID     `json:"batch_id"`
	AccountID        uuid.UUID     `json:"account_id"`
	IsnSlug          string        `json:"isn_slug"`
	CreatedAt        time.Time     `json:"created_at"`
	ClosedAt         *time.Time    `json:"closed_at,omitempty"`
	IsLatest         bool          `json:"is_latest"`
	ContainsFailures bool          `json:"contains_failures"`
	BatchStatus      []BatchStatus `json:"batch_status"`
}

// structs used by the batch search endpoint
type BatchSearchParams struct {
	IsnSlug       string
	Latest        *bool
	Previous      *bool
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	ClosedAfter   *time.Time
	ClosedBefore  *time.Time
}

// CreateSignalsBatchHandler godoc
//
//	@Summary		Create a new signal batch
//	@Description	This endpoint is used by service accounts to create a new batch. Batches are used to track signals sent by an account to the specified ISN.
//	@Description
//	@Description	Opening a batch closes the previous batch (the client app can decide how long to keep a batch open)
//	@Description
//	@Description	Signals can only be sent to open batches.
//	@Description
//	@Description	Authentication is based on the supplied access token:
//	@Description	the site owner, the isn admin and members with an isn_perm=write can create a batch for the ISN.
//	@Description
//	@Description	Note this endpoint is not needed for web users (a batch is automatically created when they first write to an isn and is only closed if their permission to write to the ISN is revoked)
//	@Description
//	@Tags		Signal sharing
//
//	@Success	201	{object}	CreateSignalsBatchResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/isn/{isn_slug}/batches [post]
//
// CreateSignalsBatchHandler must be used with the RequireValidAccessToken and RequireIsnPermission middleware functions
func (s *SignalsBatchHandler) CreateSignalsBatchHandler(w http.ResponseWriter, r *http.Request) {
	logger := zerolog.Ctx(r.Context())

	// these checks have been done already in the middleware so - if there is an error here - it is a bug.
	_, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, " could not get claims from context")
		return
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}
	account, err := s.queries.GetAccountByID(r.Context(), accountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("could not get account %v from datababase: %v ", accountID, err))
		return
	}

	if account.AccountType == "user" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "this endpoint is only for service accounts")
		return
	}

	// check isn exists
	isnSlug := r.PathValue("isn_slug")
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get ISN %v from database: %v", isnSlug, err))
		return
	}

	_, err = s.queries.CloseISNSignalBatchByAccountID(r.Context(), database.CloseISNSignalBatchByAccountIDParams{
		IsnID:     isn.ID,
		AccountID: accountID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not close open batch for user %v : %v", accountID, err))
		return
	}

	returnedRow, err := s.queries.CreateSignalBatch(r.Context(), database.CreateSignalBatchParams{
		IsnID:     isn.ID,
		AccountID: account.ID,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert signal_batch: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/%s/account/%s/batch/%s",
		utils.GetScheme(r),
		r.Host,
		isnSlug,
		account.ID,
		returnedRow.ID,
	)

	logger.Info().Msgf("New signal batch %v created by %v ", account.ID, returnedRow.ID)
	responses.RespondWithJSON(w, http.StatusOK, CreateSignalsBatchResponse{
		ResourceURL:    resourceURL,
		AccountID:      account.ID,
		SignalsBatchID: returnedRow.ID,
	})
}

// GetSignalBatchStatusHandler godocs
//
//	@Summary		Get batch processing status
//	@Description	Returns the status of a batch, including the number of signals loaded and the number of failures for each signal type
//	@Description
//	@Description	The endpoint returns the full batch status for the batch
//	@Description
//	@Description	Where a signal has failed to load as part of the batch and not subsequently been loaded, the failure is considered unresolved and listed as a failure in the batch status
//	@Description
//	@Description	Only admins/owners can use this endpoint (admins can only see status for batches they created)
//	@Description
//	@Description
//	@Tags		Signal sharing
//	@Param		isn_slug	path		string	true	"ISN slug"	example(sample-isn--example-org)
//	@Param		batch_id	path		string	true	"Batch ID"	example(67890684-3b14-42cf-b785-df28ce570400)
//	@Success	200			{object}	BatchStatusResponse
//	@Failure	400			{object}	responses.ErrorResponse
//	@Failure	404			{object}	responses.ErrorResponse
//	@Failure	500			{object}	responses.ErrorResponse
//	@Router		/isn/{isn_slug}/batches/{batch_id}/status [get]
//
// todo handle the fact that the list of failed local_refs might be very large if errors go undetected for a long time
func (s *SignalsBatchHandler) GetSignalBatchStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Extract path parameters
	batchIDString := r.PathValue("batch_id")

	// Parse batch ID
	batchID, err := uuid.Parse(batchIDString)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid batch_id format")
		return
	}

	signalBatch, err := s.queries.GetSignalBatchByID(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "batch not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// Access control rules:
	// - Member accounts can only see batches they have created
	// - ISN Admins can see batches for ISNs they administer
	// - Site owner can see all batches
	claims, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	// Get the ISN to check if user is an ISN admin
	isn, err := s.queries.GetIsnBySlug(r.Context(), r.PathValue("isn_slug"))
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// Check access permissions
	isBatchCreator := signalBatch.AccountID == claims.AccountID
	isIsnAdmin := isn.UserAccountID == claims.AccountID
	isSiteOwner := claims.Role == "owner"

	if !isBatchCreator && !isIsnAdmin && !isSiteOwner {
		responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeForbidden, "you do not have permission to view this batch")
		return
	}

	// Get batch status details using shared helper function
	response, err := s.getBatchStatusDetails(r.Context(), batchID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to get batch status: %v", err))
		return
	}

	responses.RespondWithJSON(w, http.StatusOK, response)
}

// getBatchStatusDetails gets the full status details for a batch (successful signals + failures)
func (s *SignalsBatchHandler) getBatchStatusDetails(ctx context.Context, batchID uuid.UUID) (*BatchStatusResponse, error) {

	signalBatch, err := s.queries.GetSignalBatchByID(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch: %w", err)
	}

	// Get successful signals by type
	loadedSignalSummaryRows, err := s.queries.GetLoadedSignalsSummaryByBatchID(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get successful signals: %w", err)
	}

	// Get batch failures summary (unresolved failures only)
	failedSignalRows, err := s.queries.GetFailedSignalsByBatchID(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch failures: %w", err)
	}

	// Build the response structure (reusing existing logic)
	batchSummary := make(map[string]BatchStatus)

	// Process successful loads
	for _, successes := range loadedSignalSummaryRows {
		key := successes.SignalTypeSlug + "/" + successes.SignalTypeSemVer
		batchSummary[key] = BatchStatus{
			SignalTypeSlug:     successes.SignalTypeSlug,
			SignalTypeVersion:  "v" + successes.SignalTypeSemVer,
			StoredCount:        successes.SubmittedCount,
			UnresolvedFailures: []FailureRow{},
		}
	}

	// loop through failed signals and add to the batch summary (indexed by signal type)
	for _, failures := range failedSignalRows {
		key := failures.SignalTypeSlug + "/" + failures.SignalTypeSemVer

		status := batchSummary[key] // New entry
		if status.SignalTypeSlug == "" {
			status.SignalTypeSlug = failures.SignalTypeSlug
			status.SignalTypeVersion = "v" + failures.SignalTypeSemVer
			status.StoredCount = 0
		}

		status.UnresolvedFailures = append(status.UnresolvedFailures, FailureRow{
			LocalRef:     failures.LocalRef,
			ErrorCode:    failures.ErrorCode,
			ErrorMessage: failures.ErrorMessage,
		})
		status.FailedCount++
		batchSummary[key] = status
	}

	// Convert to slice
	batchStatus := make([]BatchStatus, 0, len(batchSummary))
	for _, status := range batchSummary {
		batchStatus = append(batchStatus, status)
	}

	// Check if batch contains failures
	containsFalures := len(failedSignalRows) > 0

	// Set batch-specific fields
	var closedAt *time.Time
	if !signalBatch.IsLatest {
		closedAt = &signalBatch.UpdatedAt
	}

	return &BatchStatusResponse{
		BatchID:          batchID,
		AccountID:        signalBatch.AccountID,
		IsnSlug:          signalBatch.IsnSlug,
		CreatedAt:        signalBatch.CreatedAt,
		ClosedAt:         closedAt,
		IsLatest:         signalBatch.IsLatest,
		ContainsFailures: containsFalures,
		BatchStatus:      batchStatus,
	}, nil
}

// SearchBatchesHandler godocs
//
//	@Summary		Search for batches
//	@Tags			Signal sharing
//
//	@Description	Search for batches with optional filtering parameters
//	@Description
//	@Description	The search endpoint returns the full batch status for each batch.
//	@Description
//	@Description	Where a signal has failed to load as part of the batch and not subsequently been loaded, the failure is considered unresolved and listed as a failure in the batch status
//	@Description
//	@Description	Member accounts can only see batches they have created. ISN Admins can see batches for ISNs they administer. The site owner can see all batches.
//	@Description
//	@Description	At least one search criteria must be provided:
//	@Description	- latest=true (get the latest batch)
//	@Description	- previous=true (get the previous batch)
//	@Description	- created date range
//	@Description	- closed date range
//	@Description
//
//	@Param			latest			query		boolean	false	"Get the latest batch"						example(true)
//	@Param			previous		query		boolean	false	"Get the previous batch"					example(true)
//	@Param			created_after	query		string	false	"Start date for batch creation filtering"	example(2006-01-02T15:04:05Z)
//	@Param			created_before	query		string	false	"End date for batch creation filtering"		example(2006-01-02T16:00:00Z)
//	@Param			closed_after	query		string	false	"Start date for batch closure filtering"	example(2006-01-02T15:04:05Z)
//	@Param			closed_before	query		string	false	"End date for batch closure filtering"		example(2006-01-02T16:00:00Z)
//
//	@Success		200				{array}		BatchStatusResponse
//	@Failure		400				{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/batches/search [get]
func (s *SignalsBatchHandler) SearchBatchesHandler(w http.ResponseWriter, r *http.Request) {
	// Extract path parameters
	isnSlug := r.PathValue("isn_slug")

	// check isn exists
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// Initialize search parameters
	searchParams := BatchSearchParams{
		IsnSlug: isnSlug,
	}

	// Parse query parameters following signals pattern
	if latestString := r.URL.Query().Get("latest"); latestString != "" {
		latest := latestString == "true"
		searchParams.Latest = &latest
	}

	if previousString := r.URL.Query().Get("previous"); previousString != "" {
		previous := previousString == "true"
		searchParams.Previous = &previous
	}

	if createdAfterString := r.URL.Query().Get("created_after"); createdAfterString != "" {
		createdAfter, err := utils.ParseDateTime(createdAfterString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.CreatedAfter = &createdAfter
	}

	if createdBeforeString := r.URL.Query().Get("created_before"); createdBeforeString != "" {
		createdBefore, err := utils.ParseDateTime(createdBeforeString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.CreatedBefore = &createdBefore
	}

	if closedAfterString := r.URL.Query().Get("closed_after"); closedAfterString != "" {
		closedAfter, err := utils.ParseDateTime(closedAfterString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.ClosedAfter = &closedAfter
	}

	if closedBeforeString := r.URL.Query().Get("closed_before"); closedBeforeString != "" {
		closedBefore, err := utils.ParseDateTime(closedBeforeString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.ClosedBefore = &closedBefore
	}

	// Get authentication claims for permission checking
	claims, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	// Check that latest/previous values are actually "true" if provided
	if latestString := r.URL.Query().Get("latest"); latestString != "" && latestString != "true" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "latest parameter must be 'true' if provided")
		return
	}

	if previousString := r.URL.Query().Get("previous"); previousString != "" && previousString != "true" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "previous parameter must be 'true' if provided")
		return
	}

	// Validation: latest and previous are mutually exclusive
	if searchParams.Latest != nil && *searchParams.Latest && searchParams.Previous != nil && *searchParams.Previous {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "latest and previous parameters are mutually exclusive")
		return
	}

	// Validation: date ranges must be complete (both start and end)
	hasPartialCreatedRange := (searchParams.CreatedAfter != nil) != (searchParams.CreatedBefore != nil)
	hasPartialClosedRange := (searchParams.ClosedAfter != nil) != (searchParams.ClosedBefore != nil)

	if hasPartialCreatedRange {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "you must supply both created_after and created_before parameters")
		return
	}

	if hasPartialClosedRange {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "you must supply both closed_after and closed_before parameters")
		return
	}

	// Validation: At least one search criteria must be provided
	hasLatestOrPrevious := (searchParams.Latest != nil && *searchParams.Latest) || (searchParams.Previous != nil && *searchParams.Previous)
	hasCreatedRange := searchParams.CreatedAfter != nil && searchParams.CreatedBefore != nil
	hasClosedRange := searchParams.ClosedAfter != nil && searchParams.ClosedBefore != nil

	if !hasLatestOrPrevious && !hasCreatedRange && !hasClosedRange {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "at least one search criteria must be provided: latest=true, previous=true, created date range, or closed date range")
		return
	}

	// Access control rules:
	// - Member accounts can only see batches they have created
	// - ISN Admins can see batches for ISNs they administer
	// - Site owner can see all batches
	isAdmin := claims.Role == "owner" || isn.UserAccountID == claims.AccountID
	var requestingAccountID *uuid.UUID
	if !isAdmin {
		requestingAccountID = &claims.AccountID
	}

	// when isAdmin is true, all batches are returned for the isn, otherwise only batches for the requesting account are returned
	batches, err := s.queries.GetBatchesWithOptionalFilters(r.Context(), database.GetBatchesWithOptionalFiltersParams{
		IsnSlug:             searchParams.IsnSlug,
		RequestingAccountID: requestingAccountID,
		IsAdmin:             &isAdmin,
		CreatedAfter:        searchParams.CreatedAfter,
		CreatedBefore:       searchParams.CreatedBefore,
		ClosedAfter:         searchParams.ClosedAfter,
		ClosedBefore:        searchParams.ClosedBefore,
		Latest:              searchParams.Latest,
		Previous:            searchParams.Previous,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to search batches: %v", err))
		return
	}

	// Get full status details for each batch
	batchStatusList := make([]BatchStatusResponse, 0, len(batches))

	for _, batch := range batches {
		statusResponse, err := s.getBatchStatusDetails(r.Context(), batch.BatchID)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to get status for batch %s: %v", batch.BatchID, err))
			return
		}

		batchStatusList = append(batchStatusList, *statusResponse)
	}

	responses.RespondWithJSON(w, http.StatusOK, batchStatusList)
}
