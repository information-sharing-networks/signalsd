package handlers

import (
	"encoding/json"
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
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/jackc/pgx/v5"
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

// Response structs for GET handlers
type IsnAccount struct {
	ID                 uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	CreatedAt          time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt          time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
	IsnID              uuid.UUID `json:"isn_id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	AccountID          uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	Permission         string    `json:"permission" example:"write" enums:"read,write"`
	AccountType        string    `json:"account_type" example:"user" enums:"user,service_account"`
	IsActive           bool      `json:"is_active" example:"true"`
	Email              string    `json:"email" example:"user@example.com"`
	AccountRole        string    `json:"account_role" example:"admin" enums:"owner,admin,member"`
	ClientID           *string   `json:"client_id,omitempty" example:"client-123"`
	ClientOrganization *string   `json:"client_organization,omitempty" example:"Example Organization"`
}

// GrantIsnAccountPermission godocs
//
//	@Summary		Grant ISN access permission
//	@Tags			ISN Permissions
//
//	@Description	Grant an account read or write access to an isn.
//	@Description	This end point can only be used by the site owner or the isn admin account (ISN admins can only grant permissions for ISNs they created).
//
//	@Param			request		body	handlers.GrantIsnAccountPermissionRequest	true	"permission details"
//	@Param			isn_slug	path	string										true	"isn slug"		example(sample-isn--example-org)
//	@Param			account_id	path	string										true	"account id"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/accounts/{account_id}  [put]
//
//	this handler will insert isn_accounts.
//	for target accounts that are account.account_type "user" that are granted 'write' to an isn the handler will also start a signals batch for this isn.
//	the signal batch is used to track any writes done by the user to the isn and is only closed if their permission is revoked
//	service accounts need to create their own batches at the start of each data loading session.
//
//	this handler must use the RequireRole (owner,admin) middleware
func (i *IsnAccountHandler) GrantIsnAccountHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextLogger(r.Context())
	req := GrantIsnAccountPermissionRequest{}

	// get user account id for user making request
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	// check isn exists and is owned by user making the request
	isnSlug := r.PathValue("isn_slug")

	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	// check if user is either the ISN owner or a site owner
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	isIsnOwner := isn.UserAccountID == userAccountID
	isSiteOwner := claims.Role == "owner"

	if !isIsnOwner && !isSiteOwner {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be either the ISN owner or a site owner to grant permissions")
		return
	}

	// get target account
	targetAccountIDString := r.PathValue("account_id")
	targetAccountID, err := uuid.Parse(targetAccountIDString)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid account ID: %v", err))
		return
	}

	targetAccount, err := i.queries.GetAccountByID(r.Context(), targetAccountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}

	// deny users making uncessary attempts to grant perms to themeselves
	if userAccountID == targetAccountID {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "accounts cannot grant ISN permissions to themselves")
		return
	}

	// validate request body
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if !signalsd.ValidISNPermissions[req.Permission] {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("%v is not a valid permission", req.Permission))
		return
	}

	// check if the target user already has the permission requested
	isnAccount, err := i.queries.GetIsnAccountByIsnAndAccountID(r.Context(), database.GetIsnAccountByIsnAndAccountIDParams{
		AccountID: targetAccountID,
		IsnID:     isn.ID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not read isn_accounts for %v: %v", targetAccountID, err))
		return
	}

	// determine if we are swithching an existing permission
	updateExisting := false

	if !errors.Is(err, pgx.ErrNoRows) {
		// user has permission on this isn already
		if req.Permission == isnAccount.Permission {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("account already has %v permission on isn %v", req.Permission, isnSlug))
			return
		}
		updateExisting = true // flag for update rather than create

		// remove the previous permission
		_, err := i.queries.CloseISNSignalBatchByAccountID(r.Context(), database.CloseISNSignalBatchByAccountIDParams{
			IsnID:     isn.ID,
			AccountID: targetAccountID,
		})
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error closing sigal_batches %v", err))
			return
		}
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
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not update/create an isn_account record for user account %v when adding them to isn %v : %v", targetAccountID, isnSlug, err))
		return
	}
	reqLogger.Info("userAccount granted new permission",
		slog.String("granting_user_id", userAccountID.String()),
		slog.String("permission", req.Permission),
		slog.String("target_account_id", targetAccount.ID.String()),
		slog.String("isn_slug", isnSlug))

	responses.RespondWithStatusCodeOnly(w, http.StatusCreated)
}

// RevokeIsnAccountPermission godocs
//
//	@Summary		Revoke ISN access permission
//	@Tags			ISN Permissions
//
//	@Description	Revoke an account read or write access to an isn.
//	@Description	This end point can only be used by the site owner or the isn admin account (ISN admins can only revoke permissions for ISNs they created)
//
//	@Param			isn_slug	path	string	true	"isn slug"		example(sample-isn--example-org)
//	@Param			account_id	path	string	true	"account id"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/accounts/{account_id}  [delete]
//
//	this handler must use the RequireRole (owner,admin) middlewar
func (i *IsnAccountHandler) RevokeIsnAccountHandler(w http.ResponseWriter, r *http.Request) {

	reqLogger := logger.ContextLogger(r.Context())

	// get user account id for user making request
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	// check isn exists and is owned by user making the request
	isnSlug := r.PathValue("isn_slug")

	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	// check if user is either the ISN owner or a site owner
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	isIsnOwner := isn.UserAccountID == userAccountID
	isSiteOwner := claims.Role == "owner"

	if !isIsnOwner && !isSiteOwner {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be either the ISN owner or a site owner to revoke permissions")
		return
	}

	// get target account
	targetAccountIDString := r.PathValue("account_id")
	targetAccountID, err := uuid.Parse(targetAccountIDString)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid account ID: %v", err))
		return
	}

	targetAccount, err := i.queries.GetAccountByID(r.Context(), targetAccountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}

	// deny users making uncessary attempts to revoke perms to themeselves
	if userAccountID == targetAccountID {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("User account ID: %v cannot revoke ISN permissions for its own account", userAccountID))
		return
	}

	// check if the target user has an ISN permission to revoke
	_, err = i.queries.GetIsnAccountByIsnAndAccountID(r.Context(), database.GetIsnAccountByIsnAndAccountIDParams{
		AccountID: targetAccountID,
		IsnID:     isn.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("account %v does not have any permission to use ISN %v already - no action needed", userAccountID, isnSlug))
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not read isn_accounts for %v: %v", targetAccountID, err))
		return
	}

	// close any signal batches
	_, err = i.queries.CloseISNSignalBatchByAccountID(r.Context(), database.CloseISNSignalBatchByAccountIDParams{
		IsnID:     isn.ID,
		AccountID: targetAccountID,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error closing sigal_batches %v", err))
		return
	}

	// remove isn account permission
	rowsDeleted, err := i.queries.DeleteIsnAccount(r.Context(), database.DeleteIsnAccountParams{
		IsnID:     isn.ID,
		AccountID: targetAccountID,
	})
	if err != nil || rowsDeleted == 0 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error removing isn_account record: %v", err))
		return
	}

	reqLogger.Info("userAccount revoked permission",
		slog.String("revoking_user_id", userAccountID.String()),
		slog.String("isn_slug", isnSlug),
		slog.String("target_account_id", targetAccount.ID.String()))

	responses.RespondWithStatusCodeOnly(w, http.StatusCreated)
}

// GetIsnAccountsHandler godoc
//
//	@Summary		Get all accounts with access to an ISN
//	@Tags			ISN Permissions
//	@Description	Get a list of all accounts (users and service accounts) that have permissions on the specified ISN.
//	@Description	Only ISN admins and site owners can view this information
//
//	@Param			isn_slug	path		string	true	"ISN slug"	example(sample-isn--example-org)
//
//	@Success		200			{array}		handlers.IsnAccount
//	@Failure		400			{object}	responses.ErrorResponse
//	@Failure		403			{object}	responses.ErrorResponse
//	@Failure		404			{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/accounts [get]
func (i *IsnAccountHandler) GetIsnAccountsHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextLogger(r.Context())

	// get user account id for user making request
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	// check isn exists
	isnSlug := r.PathValue("isn_slug")

	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// check if user is either the ISN owner or a site owner
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	isIsnOwner := isn.UserAccountID == userAccountID
	isSiteOwner := claims.Role == "owner"

	if !isIsnOwner && !isSiteOwner {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be either the ISN owner or a site owner to access this resource")
		return
	}

	// Get all accounts with access to this ISN
	dbAccounts, err := i.queries.GetAccountsByIsnID(r.Context(), isn.ID)
	if err != nil {
		reqLogger.Error("database error retrieving accounts for ISN",
			slog.String("error", err.Error()),
			slog.String("isn_slug", isnSlug))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// Convert database structs to our response structs
	accounts := make([]IsnAccount, len(dbAccounts))
	for i, dbAccount := range dbAccounts {
		accounts[i] = IsnAccount{
			ID:                 dbAccount.ID,
			CreatedAt:          dbAccount.CreatedAt,
			UpdatedAt:          dbAccount.UpdatedAt,
			IsnID:              dbAccount.IsnID,
			AccountID:          dbAccount.AccountID,
			Permission:         dbAccount.Permission,
			AccountType:        dbAccount.AccountType,
			IsActive:           dbAccount.IsActive,
			Email:              dbAccount.Email,
			AccountRole:        dbAccount.AccountRole,
			ClientID:           dbAccount.ClientID,
			ClientOrganization: dbAccount.ClientOrganization,
		}
	}

	reqLogger.Info("retrieved accounts for ISN",
		slog.Int("count", len(accounts)),
		slog.String("isn_slug", isnSlug))
	responses.RespondWithJSON(w, http.StatusOK, accounts)
}
