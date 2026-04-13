package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/utils"
	"github.com/jackc/pgx/v5"
)

type SignalsBatchHandler struct {
	queries *database.Queries
}

func NewSignalsBatchHandler(queries *database.Queries) *SignalsBatchHandler {
	return &SignalsBatchHandler{queries: queries}
}

// FailureRow represents a single unresolved failure within a batch
type FailureRow struct {
	LocalRef     string `json:"local_ref"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// BatchStatus summarises stored and failed signals for one ISN + signal type combination within a batch
type BatchStatus struct {
	IsnSlug            string       `json:"isn_slug"`
	SignalTypeSlug     string       `json:"signal_type_slug"`
	SignalTypeVersion  string       `json:"signal_type_version"`
	StoredCount        int64        `json:"stored_count"`
	RejectedCount      int64        `json:"rejected_count"`
	UnresolvedFailures []FailureRow `json:"unresolved_failures,omitempty"`
}

// BatchStatusResponse is the full status for a batch across all ISNs and signal types
type BatchStatusResponse struct {
	BatchID          uuid.UUID     `json:"batch_id"`
	BatchRef         string        `json:"batch_ref"`
	AccountID        uuid.UUID     `json:"account_id"`
	CreatedAt        time.Time     `json:"created_at"`
	ContainsFailures bool          `json:"contains_failures"`
	BatchStatus      []BatchStatus `json:"batch_status"`
}

// BatchSearchParams holds validated query parameters for batch search
type BatchSearchParams struct {
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
}

// GetSignalBatchStatus godoc
//
//	@Summary		Get Batch Status
//
//	@Description	Returns the status of a batch identified by batch_ref, scoped to the authenticated account.
//	@Description
//	@Description	The response shows stored and failed signal counts broken down by ISN and signal type.
//	@Description	Where a signal is listed as 'rejected' this means the signal failed to load in this batch and has not been successfully resubmitted subsequently.
//	@Description
//	@Description	Members can view their own batches. Site admins can supply ?account_id= to view another account's batch.
//	@Description
//
//	@Tags		Signal Exchange
//	@Param		batch_ref	path		string	true	"Batch reference"	example(daily-sync-2026-04-02)
//	@Param		account_id	query		string	false	"Account ID (site admins only)"
//
//	@Success	200			{object}	BatchStatusResponse
//	@Failure	400			{object}	responses.ErrorResponse
//	@Failure	403			{object}	responses.ErrorResponse
//	@Failure	404			{object}	responses.ErrorResponse
//	@Failure	500			{object}	responses.ErrorResponse
//
//	@Security	BearerAccessToken
//	@Router		/api/batches/{batch_ref}/status [get]
func (s *SignalsBatchHandler) GetSignalBatchStatus(w http.ResponseWriter, r *http.Request) {

	batchRef := r.PathValue("batch_ref")

	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	// Site admins may supply ?account_id= to view another account's batch; everyone else sees their own.
	resolvedAccountID := claims.AccountID
	if accountIDString := r.URL.Query().Get("account_id"); accountIDString != "" {
		if claims.Role != "siteadmin" {
			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "only site admins can view other accounts' batches")
			return
		}
		parsed, err := uuid.Parse(accountIDString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "invalid account_id format")
			return
		}
		resolvedAccountID = parsed
	}

	signalBatch, err := s.queries.GetSignalBatchByRefAndAccountID(r.Context(), database.GetSignalBatchByRefAndAccountIDParams{
		AccountID: resolvedAccountID,
		BatchRef:  batchRef,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "batch not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("batch_ref", batchRef),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	response, err := s.getBatchStatusDetails(r.Context(), signalBatch.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("batch_ref", batchRef),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	responses.RespondWithJSON(w, http.StatusOK, response)
}

// getBatchStatusDetails returns the full status for a batch across all ISNs and signal types.
func (s *SignalsBatchHandler) getBatchStatusDetails(ctx context.Context, batchID uuid.UUID) (*BatchStatusResponse, error) {

	signalBatch, err := s.queries.GetSignalBatchByID(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch: %w", err)
	}

	loadedRows, err := s.queries.GetLoadedSignalsSummaryByBatchID(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get successful signals: %w", err)
	}

	failedRows, err := s.queries.GetFailedSignalsByBatchID(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch failures: %w", err)
	}

	// key on isn_slug + signal_type + version so a batch spanning multiple ISNs shows correctly
	batchSummary := make(map[string]BatchStatus)

	for _, row := range loadedRows {
		key := row.IsnSlug + "/" + row.SignalTypeSlug + "/" + row.SignalTypeSemVer
		batchSummary[key] = BatchStatus{
			IsnSlug:            row.IsnSlug,
			SignalTypeSlug:     row.SignalTypeSlug,
			SignalTypeVersion:  "v" + row.SignalTypeSemVer,
			StoredCount:        row.SubmittedCount,
			UnresolvedFailures: []FailureRow{},
		}
	}

	for _, row := range failedRows {
		key := row.IsnSlug + "/" + row.SignalTypeSlug + "/" + row.SignalTypeSemVer
		status := batchSummary[key]
		if status.SignalTypeSlug == "" {
			status.IsnSlug = row.IsnSlug
			status.SignalTypeSlug = row.SignalTypeSlug
			status.SignalTypeVersion = "v" + row.SignalTypeSemVer
		}
		status.UnresolvedFailures = append(status.UnresolvedFailures, FailureRow{
			LocalRef:     row.LocalRef,
			ErrorCode:    row.ErrorCode,
			ErrorMessage: row.ErrorMessage,
		})
		status.RejectedCount++
		batchSummary[key] = status
	}

	batchStatus := make([]BatchStatus, 0, len(batchSummary))
	for _, status := range batchSummary {
		batchStatus = append(batchStatus, status)
	}

	return &BatchStatusResponse{
		BatchID:          batchID,
		BatchRef:         signalBatch.BatchRef,
		AccountID:        signalBatch.AccountID,
		CreatedAt:        signalBatch.CreatedAt,
		ContainsFailures: len(failedRows) > 0,
		BatchStatus:      batchStatus,
	}, nil
}

// SearchBatches godoc
//
//	@Summary		Search For Batches
//	@Tags			Signal Exchange
//
//	@Description	Returns the full status for all matching batches. Members see their own batches; site admins see all.
//	@Description	At least one date filter must be provided.
//
//	@Param			created_after	query		string	false	"Earliest batch creation time"	example(2006-01-02T15:04:05Z)
//	@Param			created_before	query		string	false	"Latest batch creation time"	example(2006-01-02T16:00:00Z)
//
//	@Success		200				{array}		BatchStatusResponse
//	@Failure		400				{object}	responses.ErrorResponse
//	@Failure		500				{object}	responses.ErrorResponse
//	@Security		BearerAccessToken
//	@Router			/api/batches/search [get]
func (s *SignalsBatchHandler) SearchBatches(w http.ResponseWriter, r *http.Request) {

	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	var searchParams BatchSearchParams

	if v := r.URL.Query().Get("created_after"); v != "" {
		t, err := utils.ParseDateTime(v)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.CreatedAfter = &t
	}

	if v := r.URL.Query().Get("created_before"); v != "" {
		t, err := utils.ParseDateTime(v)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, err.Error())
			return
		}
		searchParams.CreatedBefore = &t
	}

	if (searchParams.CreatedAfter != nil) != (searchParams.CreatedBefore != nil) {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "supply both created_after and created_before, or neither")
		return
	}

	if searchParams.CreatedAfter == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "at least one date range filter must be provided")
		return
	}

	isAdmin := claims.Role == "siteadmin"
	var requestingAccountID *uuid.UUID
	if !isAdmin {
		requestingAccountID = &claims.AccountID
	}

	batches, err := s.queries.GetBatchesWithOptionalFilters(r.Context(), database.GetBatchesWithOptionalFiltersParams{
		RequestingAccountID: requestingAccountID,
		IsAdmin:             &isAdmin,
		CreatedAfter:        searchParams.CreatedAfter,
		CreatedBefore:       searchParams.CreatedBefore,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	batchStatusList := make([]BatchStatusResponse, 0, len(batches))
	for _, batch := range batches {
		statusResponse, err := s.getBatchStatusDetails(r.Context(), batch.BatchID)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
				slog.String("batch_id", batch.BatchID.String()),
			)
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
		batchStatusList = append(batchStatusList, *statusResponse)
	}

	responses.RespondWithJSON(w, http.StatusOK, batchStatusList)
}
