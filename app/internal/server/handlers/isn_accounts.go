package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/jackc/pgx/v5"
)

type IsnAccountHandler struct {
	queries *database.Queries
}

func NewIsnAccountHandler(queries *database.Queries) *IsnAccountHandler {
	return &IsnAccountHandler{queries: queries}
}

type UpdateIsnAccountPermissionRequest struct {
	CanRead  *bool `json:"can_read" example:"true"`
	CanWrite *bool `json:"can_write" example:"false"`
}

// Response structs for GET handlers
type IsnAccount struct {
	ID                 uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	CreatedAt          time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt          time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
	IsnID              uuid.UUID `json:"isn_id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	AccountID          uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	CanRead            bool      `json:"can_read" example:"true"`
	CanWrite           bool      `json:"can_write" example:"false"`
	AccountType        string    `json:"account_type" example:"user" enums:"user,service_account"`
	IsActive           bool      `json:"is_active" example:"true"`
	Email              string    `json:"email" example:"user@example.com"`
	AccountRole        string    `json:"account_role" example:"isnadmin" enums:"siteadmin,isnadmin,member"`
	ClientID           *string   `json:"client_id,omitempty" example:"sa_exampleorg_k7j2m9x1"`
	ClientOrganization *string   `json:"client_organization,omitempty" example:"Example Organization"`
}

// UpdateIsnAccountPermission godocs
//
//	@Summary		Grant/Revoke ISN Access
//	@Tags			Account Management
//
//	@Description	Update an account's access permission for an ISN. Set both can_read and can_write to false to revoke all access.
//	@Description
//	@Description	This endpoint can only be used by admin accounts:
//	@Description	- ISN admins can only update membership for ISNs they created).
//	@Description	- Site admins can update membership for any ISN
//	@Description
//	@Description	Permissions:
//	@Description	- Accounts with 'read' permission can view all signals on the ISN.
//	@Description	- Accounts with 'write' permission can create signals on the ISN.
//	@Description	- For accounts that need read/write access to an ISN, you must grant both 'read' and 'write' permissions.
//	@Description
//	@Description	Note that accounts with 'write' permission to an ISN are also automatically granted 'read'
//	@Description	permission for signals they created, but can't view other signals on the ISN.
//	@Description
//	@Description	You must supply values for both can_read and can_write.
//
//	@Param			request		body	handlers.UpdateIsnAccountPermissionRequest	true	"permission details"
//	@Param			isn_slug	path	string										true	"isn slug"		example(sample-isn)
//	@Param			account_id	path	string										true	"account id"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		200
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/accounts/{account_id}  [put]
//
//	this handler must use the RequireRole (siteadmin,admin) middleware
func (i *IsnAccountHandler) UpdateIsnAccountPermissionHandler(w http.ResponseWriter, r *http.Request) {

	req := UpdateIsnAccountPermissionRequest{}

	// get account id for the account making request
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

		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_slug", isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	// If the requester is an ISN admin, they must own this ISN.
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	if claims.Role == "isnadmin" && isn.UserAccountID != userAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be the ISN owner to grant permissions")
		return
	}

	// get target account
	targetAccountIDString := r.PathValue("account_id")
	targetAccountID, err := uuid.Parse(targetAccountIDString)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "invalid account ID format")
		return
	}

	targetAccount, err := i.queries.GetAccountByID(r.Context(), targetAccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("target_account_id", targetAccountID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// deny users making uncessary attempts to grant perms to themeselves
	if userAccountID == targetAccountID {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("operation", "grant_isn_account"),
			slog.String("error", "accounts cannot grant ISN permissions to themselves"),
		)
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "accounts cannot grant ISN permissions to themselves")
		return
	}

	// validate request body
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	if req.CanRead == nil || req.CanWrite == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply values for both can_read and can_write")
		return
	}

	// get the current permissions for the target account on the ISN
	isnAccount, err := i.queries.GetIsnAccountByIsnAndAccountID(r.Context(), database.GetIsnAccountByIsnAndAccountIDParams{
		AccountID: targetAccountID,
		IsnID:     isn.ID,
	})

	isExistingPermissions := err == nil

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("target_account_id", targetAccountID.String()),
			slog.String("isn_slug", isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Handle batch closure when revoking write access - this is just to tidy up the database - it's not an error if there is no batch to close
	if isExistingPermissions && isnAccount.CanWrite && !*req.CanWrite {
		_, err = i.queries.CloseSignalBatchByIsnIdAndAccountID(r.Context(), database.CloseSignalBatchByIsnIdAndAccountIDParams{
			IsnID:     isn.ID,
			AccountID: targetAccountID,
		})
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
				slog.String("target_account_id", targetAccountID.String()),
				slog.String("isn_slug", isnSlug),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
	}

	// Create or update the ISN account permission
	if isExistingPermissions {
		_, err = i.queries.UpdateIsnAccount(r.Context(), database.UpdateIsnAccountParams{
			IsnID:     isn.ID,
			AccountID: targetAccountID,
			CanRead:   *req.CanRead,
			CanWrite:  *req.CanWrite,
		})
	} else {
		_, err = i.queries.CreateIsnAccount(r.Context(), database.CreateIsnAccountParams{
			IsnID:     isn.ID,
			AccountID: targetAccountID,
			CanRead:   *req.CanRead,
			CanWrite:  *req.CanWrite,
		})
	}
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("target_account_id", targetAccountID.String()),
			slog.String("isn_slug", isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	logger.ContextWithLogAttrs(r.Context(),
		slog.Bool("can_read", *req.CanRead),
		slog.Bool("can_write", *req.CanWrite),
		slog.String("target_account_id", targetAccount.ID.String()),
		slog.String("isn_slug", isnSlug),
	)

	responses.RespondWithStatusCodeOnly(w, http.StatusOK)
}

// GetIsnAccountsHandler godoc
//
//	@Summary		Get ISN Account Membership
//	@Tags			ISN Configuration
//	@Description	Get a list of all accounts (users and service accounts) that have permissions on the specified ISN.
//	@Description	Only ISN admins and site owners can view this information
//
//	@Param			isn_slug	path		string	true	"ISN slug"	example(sample-isn)
//
//	@Success		200			{array}		handlers.IsnAccount
//	@Failure		400			{object}	responses.ErrorResponse
//	@Failure		403			{object}	responses.ErrorResponse
//	@Failure		404			{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/accounts [get]
//
// this handler must use the RequireRole (siteadmin,admin) middleware
func (i *IsnAccountHandler) GetIsnAccountsHandler(w http.ResponseWriter, r *http.Request) {

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
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_slug", isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// If the requester is an ISN admin, they must own this ISN.
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	if claims.Role == "isnadmin" && isn.UserAccountID != userAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be the ISN owner to access this resource")
		return
	}

	// Get all accounts with access to this ISN
	dbAccounts, err := i.queries.GetAccountsByIsnID(r.Context(), isn.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_slug", isnSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
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
			CanRead:            dbAccount.CanRead,
			CanWrite:           dbAccount.CanWrite,
			AccountType:        dbAccount.AccountType,
			IsActive:           dbAccount.IsActive,
			Email:              dbAccount.Email,
			AccountRole:        dbAccount.AccountRole,
			ClientID:           dbAccount.ClientID,
			ClientOrganization: dbAccount.ClientOrganization,
		}
	}

	logger.ContextWithLogAttrs(r.Context(),
		slog.Int("count", len(accounts)),
		slog.String("isn_slug", isnSlug))
	responses.RespondWithJSON(w, http.StatusOK, accounts)
}
