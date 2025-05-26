package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/utils"
	"github.com/rs/zerolog"
)

type SignalsBatchHandler struct {
	queries *database.Queries
}

func NewSignalsBatchHandler(queries *database.Queries) *SignalsBatchHandler {
	return &SignalsBatchHandler{queries: queries}
}

/* tdo
id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE,
    isn_id UUID NOT NULL,
    account_id UUID NOT NULL,
    is_latest BOOL NOT NULL DEFAULT true,
    account_type TEXT NOT NULL,
*/

type CreateSignalsBatchRequest struct {
	IsnSlug string `json:"isn_slug" example:"sample-isn--example-org"`
}

type CreateSignalsBatchResponse struct {
	ResourceURL    string    `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org/account/{account_id}/batch/{signals_batch_id}"`
	AccountID      uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	SignalsBatchID uuid.UUID `json:"signals_batch_id" example:"b51faf05-aaed-4250-b334-2258ccdf1ff2"`
}

// CreateSignalsBatchHandler godoc
//
//	@Summary        Create a new batch for sending signals to the specified isn
//	@Description    This endpoint is only used by service accounts.
//	@Description
//	@Description    For user accounts, a batch is automatically created when they are granted write permission to an isn and is never closed)
//	@Description
//	@Description    For service accounts, the client app can decide how long to keep a batch open
//	@Description    (a batch status summary is sent to a webhook after the batch closes)
//	@Description
//	@Description	opening a batch closes the previous batch created on the isn for this account.
//	@Description
//	@Description    Signals can only be sent to open batches.
//	@Description
//	@Description    authentication is based on the supplied access token:
//	@Description    (the site owner; the isn admin and members with an isn_perm= write can create a batch)
//	@Description
//	@Description	TODO - this end point temporarily open to end users - batch should be auto created by create isn_account
//	@Tags			Signal Exchange
//
//	@Success		201		{object} 	CreateSignalsBatchResponse
//	@Failure		500		{object}	utils.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signals/batch [post]
//
// CreateSignalsBatchHandler must be used with the RequireValidAccessToken amd RequireIsnWritePermission middleware functions
func (s *SignalsBatchHandler) CreateSignalsBatchHandler(w http.ResponseWriter, r *http.Request) {
	logger := zerolog.Ctx(r.Context())

	//todo handle users returning with the same isn

	// these checks have been done already in the middleware so - if there is an error here - it is a bug.
	_, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, " could not get claims from context")
		return
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}
	account, err := s.queries.GetAccountByID(r.Context(), accountID)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get ISN from database: %v", err))
		return
	}

	// check isn exists
	isnSlug := r.PathValue("isn_slug")
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get ISN from database: %v", err))
		return
	}

	_, err = s.queries.CloseISNSignalBatchByAccountID(r.Context(), database.CloseISNSignalBatchByAccountIDParams{
		IsnID:     isn.ID,
		AccountID: accountID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get ISN from database: %v", err))
		return
	}

	returnedRow, err := s.queries.CreateSignalBatch(r.Context(), database.CreateSignalBatchParams{
		IsnID:       isn.ID,
		AccountID:   account.ID,
		AccountType: account.AccountType,
	})
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert signal_batch: %v", err))
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
	utils.RespondWithJSON(w, http.StatusOK, CreateSignalsBatchResponse{
		ResourceURL:    resourceURL,
		AccountID:      account.ID,
		SignalsBatchID: returnedRow.ID,
	})
}

// GetSignalsBatchHandler godoc todo
func (u *SignalsBatchHandler) GetSignalsBatchHandler(w http.ResponseWriter, r *http.Request) {
	utils.RespondWithJSON(w, http.StatusOK, "")
}
