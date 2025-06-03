package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/server/responses"
	"github.com/nickabs/signalsd/app/internal/server/utils"
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

// CreateSignalsBatchHandler godoc
//
//	@Summary		Create a new signal batch
//	@Description	This endpoint is used by service accounts to create a new batch used to track signals sent to the specified isn
//	@Description
//	@Description	For user accounts, a batch is automatically created when they are granted write permission to an isn and is only closed if their permission to write to the isn is revoked
//	@Description
//	@Description	For service accounts, the client app can decide how long to keep a batch open
//	@Description	(a batch status summary is sent to a webhook after the batch closes)
//	@Description
//	@Description	opening a batch closes the previous batch created on the isn for this account.
//	@Description
//	@Description	Signals can only be sent to open batches.
//	@Description
//	@Description	authentication is based on the supplied access token:
//	@Description	(the site owner; the isn admin and members with an isn_perm= write can create a batch)
//	@Description
//	@Tags		Service accounts
//
//	@Success	201	{object}	CreateSignalsBatchResponse
//	@Failure	500	{object}	responses.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/isn/{isn_slug}/batches [post]
//
// CreateSignalsBatchHandler must be used with the RequireValidAccessToken amd RequireIsnPermission middleware functions
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
		IsnID:       isn.ID,
		AccountID:   account.ID,
		AccountType: account.AccountType,
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

// GetSignalsBatchHandler godocs
//
//	@Summary		Get a signal batch
//	@Tags			Service accounts
//
//	@Description	TODO - get by id. Include status (errs, received, latest localref in batch)
//
//	@Router			/api/isn/{isn_slug}/accounts/{account_id}/batches/{signals_batch_id} [get]
func (u *SignalsBatchHandler) GetSignalsBatchHandler(w http.ResponseWriter, r *http.Request) {
	responses.RespondWithJSON(w, http.StatusOK, "")

	responses.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - not yet implemented")
}

// GetSignalsBatchHandlers godocs
//
//	@Summary		Get details about a set of signal batches
//	@Description	TODO - get latest, previous, by data ranage
//	@Tags			Service accounts
//
//	@Description	TODO
//
//	@Router			/api/isn/{isn_slug}/accounts/{account_id}/batches [get]
func (u *SignalsBatchHandler) GetSignalsBatchesHandler(w http.ResponseWriter, r *http.Request) {
	responses.RespondWithJSON(w, http.StatusOK, "")

	responses.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - not yet implemented")
}
