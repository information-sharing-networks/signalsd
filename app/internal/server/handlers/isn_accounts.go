package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	signalsd "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/utils"
	"github.com/rs/zerolog"
)

type IsnAccountHandler struct {
	queries *database.Queries
}

func NewIsnAccountHandler(queries *database.Queries) *IsnAccountHandler {
	return &IsnAccountHandler{queries: queries}
}

type GrantIsnAccountPermissionRequest struct {
	Permission string `json:"permission" emuns:"write,read" example:"write"`
}

// GrantIsnAccountPermission godocs
//
//	@Summary	Grant ISN access permission
//	@Tags		ISN Permissions
//
//	@Description	Grant an account read or write access to an isn.
//	@Description	This end point can only be used by the site owner or the isn admin account.
//
//	@Param			isn_slug	path		string	true	"isn slug"	example(sample-isn--example-org)
//	@Param			account_id	path		string	true	"account id"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		204
//	@Failure		400			{object}	utils.ErrorResponse
//	@Failure		500			{object}	utils.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/accounts/{account_id}  [put]
//
//	this handler will insert isn_accounts.
//	for target accounts that are account.account_type "user" that are granted 'write' to an isn the handler will also start a signals batch for this isn.
//	the signal batch is used to track any writes done by the user to the isn and is only closed if their permission is revoked
//	service accounts need to create their own batches at the start of each data loading session.
//
//	this handler must use the RequireRole (owner,admin) middleware
func (i *IsnAccountHandler) GrantIsnAccountHandler(w http.ResponseWriter, r *http.Request) {
	logger := zerolog.Ctx(r.Context())
	req := GrantIsnAccountPermissionRequest{}

	// get user account id for user making request
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	// check isn exists and is owned by user making the request
	isnSlug := r.PathValue("isn_slug")

	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserAccountID != userAccountID {
		utils.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	// get target account
	targetAccountIDString := r.PathValue("account_id")
	targetAccountID, err := uuid.Parse(targetAccountIDString)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid account ID: %v", err))
		return
	}

	targetAccount, err := i.queries.GetAccountByID(r.Context(), targetAccountID)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}

	// deny users making uncessary attempts to grant perms to themeselves
	if userAccountID == targetAccountID {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("User account ID: %v cannot grant ISN permissions to its own account", userAccountID))
		return
	}

	// validate request body
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if !signalsd.ValidISNPermissions[req.Permission] {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("%v is not a valid permission", req.Permission))
		return
	}

	// check if the target user already has the permission requested
	isnAccount, err := i.queries.GetIsnAccountByIsnAndAccountID(r.Context(), database.GetIsnAccountByIsnAndAccountIDParams{
		AccountID: targetAccountID,
		IsnID:     isn.ID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not read isn_accounts for %v: %v", targetAccountID, err))
		return
	}

	// determine if we are swithching an existing permission
	updateExisting := false

	if !errors.Is(err, sql.ErrNoRows) {
		// user has permission on this isn already
		if req.Permission == isnAccount.Permission {
			utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("%v already has %v permission on isn %v", targetAccountID, req.Permission, isnSlug))
			return
		}
		updateExisting = true // flag for update rather than create

		// remove the previous permission
		_, err := i.queries.CloseISNSignalBatchByAccountID(r.Context(), database.CloseISNSignalBatchByAccountIDParams{
			IsnID:     isn.ID,
			AccountID: targetAccountID,
		})
		if err != nil {
			utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error closing sigal_batches %v", err))
			return
		}
	}

	// create a new batch for users being added to the isn as writes
	if targetAccount.AccountType == "user" && req.Permission == "write" {

		// create new batch
		returnedRow, err := i.queries.CreateSignalBatch(r.Context(), database.CreateSignalBatchParams{
			IsnID:       isn.ID,
			AccountID:   targetAccountID,
			AccountType: "user",
		})
		if err != nil {
			utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create a signals_batch record for user account %v when adding them to isn %v : %v", targetAccountID, isnSlug, err))
			return
		}
		logger.Info().Msgf("new signal_batch %v created for account %v on isn %v", returnedRow.ID, targetAccountID, isnSlug)
	}

	if updateExisting {
		_, err = i.queries.UpdateIsnAccount(r.Context(), database.UpdateIsnAccountParams{
			IsnID:      isn.ID,
			AccountID:  targetAccountID,
			Permission: req.Permission,
		})
	} else {
		_, err = i.queries.CreateIsnAccount(r.Context(), database.CreateIsnAccountParams{
			IsnID:      isn.ID,
			AccountID:  targetAccountID,
			Permission: req.Permission,
		})
	}
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not update/create an isn_account record for user account %v when adding them to isn %v : %v", targetAccountID, isnSlug, err))
		return
	}
	logger.Info().Msgf("userAccount %v granted new permission %v to account %v on isn %v", userAccountID, req.Permission, targetAccount.ID, isnSlug)

	utils.RespondWithJSON(w, http.StatusNoContent, "")
}

// RevokeIsnAccountPermission godocs
//
//	@Summary		Revoke ISN access permission
//	@Tags			ISN Permissions
//
//	@Description	Revoke an account read or write access to an isn.
//	@Description	This end point can only be used by the site owner or the isn admin account.
//
//	@Param			isn_slug	path		string	true	"isn slug"	example(sample-isn--example-org)
//	@Param			account_id	path		string	true	"account id"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		204
//	@Failure		400			{object}	utils.ErrorResponse
//	@Failure		500			{object}	utils.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/accounts/{account_id}  [delete]
//
//	this handler must use the RequireRole (owner,admin) middlewar
func (i *IsnAccountHandler) RevokeIsnAccountHandler(w http.ResponseWriter, r *http.Request) {

	logger := zerolog.Ctx(r.Context())

	// get user account id for user making request
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	// check isn exists and is owned by user making the request
	isnSlug := r.PathValue("isn_slug")

	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserAccountID != userAccountID {
		utils.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	// get target account
	targetAccountIDString := r.PathValue("account_id")
	targetAccountID, err := uuid.Parse(targetAccountIDString)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid account ID: %v", err))
		return
	}

	targetAccount, err := i.queries.GetAccountByID(r.Context(), targetAccountID)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}

	// deny users making uncessary attempts to revoke perms to themeselves
	if userAccountID == targetAccountID {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("User account ID: %v cannot revoke ISN permissions for its own account", userAccountID))
		return
	}
	utils.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - not yet implemented")

	// check if the target user has an ISN permission to revoke
	_, err = i.queries.GetIsnAccountByIsnAndAccountID(r.Context(), database.GetIsnAccountByIsnAndAccountIDParams{
		AccountID: targetAccountID,
		IsnID:     isn.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("account %v does not have any permission to use ISN %v already - no action needed", userAccountID, isnSlug))
			return
		}
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not read isn_accounts for %v: %v", targetAccountID, err))
		return
	}

	// close any signal batches
	_, err = i.queries.CloseISNSignalBatchByAccountID(r.Context(), database.CloseISNSignalBatchByAccountIDParams{
		IsnID:     isn.ID,
		AccountID: targetAccountID,
	})
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error closing sigal_batches %v", err))
		return
	}

	// remove isn account permission
	rowsDeleted, err := i.queries.DeleteIsnAccount(r.Context(), database.DeleteIsnAccountParams{
		IsnID:     isn.ID,
		AccountID: targetAccountID,
	})
	if err != nil || rowsDeleted == 0 {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error removing isn_account record: %v", err))
		return
	}

	logger.Info().Msgf("userAccount %v revoked permission on %v to account %v", userAccountID, isnSlug, targetAccount.ID)

	utils.RespondWithJSON(w, http.StatusNoContent, "")
}
