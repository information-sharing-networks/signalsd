package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	signalsd "github.com/information-sharing-networks/signalsd/app"
)

type UserHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
	pool        *pgxpool.Pool
}

func NewUserHandler(queries *database.Queries, authService *auth.AuthService, pool *pgxpool.Pool) *UserHandler {
	return &UserHandler{
		queries:     queries,
		authService: authService,
		pool:        pool,
	}
}

type CreateUserRequest struct {
	Password string `json:"password" example:"lkIB53@6O^Y"` // passwords must be at least 11 characters long
	Email    string `json:"email" example:"example@example.com"`
}

type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password" example:"lkIB53@6O^Y"`
	NewPassword     string `json:"new_password" example:"ue6U>&X3j570"`
}

// RegisterUserHandler godoc
//
//	@Summary		Register user
//	@Tags			auth
//
//	@Param			request	body	handlers.CreateUserRequest	true	"user details"
//	@Description	The first user to be created for this service will be created with an admin role.
//	@Description	Subsequent accounts default to standard member roles.
//	@Description	New members can't access any information beyond the public data on the site until an admin grants them access to an ISN.
//
//	@Success		201
//	@Failure		400	{object}	responses.ErrorResponse	"Bad request with possible error codes: malformed_body, password_too_short"
//	@Failure		409	{object}	responses.ErrorResponse	"Conflict with possible error code: resource_already_exists"
//
//	@Router			/auth/register [post]
func (u *UserHandler) RegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.Email == "" || req.Password == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply {email} and {password}")
		return
	}

	exists, err := u.queries.ExistsUserWithEmail(r.Context(), req.Email)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, "a user already exists this email address")
		return
	}

	if len(req.Password) < signalsd.MinimumPasswordLength {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodePasswordTooShort, fmt.Sprintf("password must be at least %d chars", signalsd.MinimumPasswordLength))
		return
	}

	hashedPassword, err := u.authService.HashPassword(req.Password)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not hash password: %v", err))
		return
	}

	// create the account record followed by the user (note transaction needed to ensure both records are created together)
	tx, err := u.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to being transaction: %v", err))
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to rollback transaction: %v", err))
			return
		}
	}()

	txQueries := u.queries.WithTx(tx)

	account, err := txQueries.CreateUserAccount(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert account record: %v", err))
		return
	}

	// first user is granted the owner role
	isFirstUser, err := txQueries.IsFirstUser(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isFirstUser {
		_, err = txQueries.CreateOwnerUser(r.Context(), database.CreateOwnerUserParams{
			AccountID:      account.ID,
			HashedPassword: hashedPassword,
			Email:          req.Email,
		})
	} else {
		_, err = txQueries.CreateUser(r.Context(), database.CreateUserParams{
			AccountID:      account.ID,
			HashedPassword: hashedPassword,
			Email:          req.Email,
		})
	}
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create user: %v", err))
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusCreated)
}

// UpdatePasswordHandler godoc
//
//	@Summary		Password reset
//	@Description	Use this api when a user wants to reset their password.  Requires a valid access token and the current password
//	@Description
//	@Tags			auth
//
//	@Param			request	body	handlers.UpdatePasswordRequest	true	"user details"
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse	"Bad request with possible error codes: malformed_body, password_too_short"
//	@Failure		401	{object}	responses.ErrorResponse	"Unauthorized with possible error code: authentication_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/auth/password/reset [put]
func (u *UserHandler) UpdatePasswordHandler(w http.ResponseWriter, r *http.Request) {
	req := UpdatePasswordRequest{}

	// this request was already authenticated by the middleware
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.NewPassword == "" || req.CurrentPassword == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply current and new password in the request")
		return
	}

	user, err := u.queries.GetUserByID(r.Context(), userAccountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error retreiving user from access code (%v) %v", userAccountID, err))
		return
	}

	currentPasswordHash, err := u.authService.HashPassword(req.CurrentPassword)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("server error: %v", err))
		return
	}

	err = u.authService.CheckPasswordHash(currentPasswordHash, req.CurrentPassword)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "Incorrect email or password")
		return
	}

	if len(req.NewPassword) < signalsd.MinimumPasswordLength {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodePasswordTooShort, fmt.Sprintf("password must be at least %d chars", signalsd.MinimumPasswordLength))
		return
	}

	newPasswordHash, err := u.authService.HashPassword(req.NewPassword)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("server error: %v", err))
		return
	}

	rowsAffected, err := u.queries.UpdatePassword(r.Context(), database.UpdatePasswordParams{
		AccountID:      user.AccountID,
		HashedPassword: newPasswordHash,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if rowsAffected != 1 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "error updating user")
		return
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusNoContent)

}

// GrantUserAdminRoleHandler godocs
//
//	@Summary		Grant admin role
//	@Tags			auth
//
//	@Description	This endpoint grants the admin role to a site member
//	@Description
//	@Description	An admin can:
//	@Description	- Create an ISN
//	@Description	- Define the signal_types used in the ISN
//	@Description	- read/write to their own ISNs
//	@Description	- Grant other accounts read or write access to their ISNs
//	@Description	- Create service accounts
//	@Description
//	@Description	this endpoint can only be used by the site owner account
//
//	@Param			account_id	path	string	true	"account id"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/auth/admins/account/{account_id} [put]
//
//	this handler must use the RequireRole (owner) middlewear
func (u *UserHandler) GrantUserAdminRoleHandler(w http.ResponseWriter, r *http.Request) {

	logger := zerolog.Ctx(r.Context())

	// get user account id for user making request
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	// get target account
	targetAccountIDString := r.PathValue("account_id")
	targetAccountID, err := uuid.Parse(targetAccountIDString)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid account ID: %v", err))
		return
	}
	targetAccount, err := u.queries.GetAccountByID(r.Context(), targetAccountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}

	// prevent owners trying to make themselves admin
	if userAccountID == targetAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "the owner account is not permitted to change its own role to admin")
		return
	}

	if targetAccount.AccountType != "user" {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this end point should not be used for service accounts")
		return
	}

	if targetAccount.AccountRole == "admin" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("%v is already an admin", targetAccountID))
		return
	}

	//update user role
	rowsUpdated, err := u.queries.UpdateUserAccountToAdmin(r.Context(), targetAccountID)
	if err != nil || rowsUpdated == 0 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}
	logger.Info().Msgf("%v updated to be an admin", targetAccountID)
	responses.RespondWithStatusCodeOnly(w, http.StatusCreated)
}

// RevokeAccountAdmin godocs
//
//	@Summary		Revoke account admin role
//	@Tags			auth
//
//	@Description	this endpoint can only be used by the site owner account
//
//	@Param			account_id	path	string	true	"account id"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/auth/admins/{account_id} [delete]
//
//	this handler must use the RequireRole (owner) middlewar
func (u *UserHandler) RevokeUserAdminRoleHandler(w http.ResponseWriter, r *http.Request) {
	logger := zerolog.Ctx(r.Context())

	// get user account id for user making request
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	// get target account
	targetAccountIDString := r.PathValue("account_id")
	targetAccountID, err := uuid.Parse(targetAccountIDString)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid account ID: %v", err))
		return
	}
	targetAccount, err := u.queries.GetAccountByID(r.Context(), targetAccountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}

	// prevent owners trying to make themselves admin
	if userAccountID == targetAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "the owner account is not permitted to change its own role")
		return
	}

	if targetAccount.AccountType != "user" {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this end point should not be used for service accounts")
		return
	}

	if targetAccount.AccountRole != "admin" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("%v is not an admin", targetAccountID))
		return
	}

	//update user role
	rowsUpdated, err := u.queries.UpdateUserAccountToMember(r.Context(), targetAccountID)
	if err != nil || rowsUpdated == 0 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get account %v from database: %v", targetAccountID, err))
		return
	}
	logger.Info().Msgf("%v: admin role revoked", targetAccountID)
	responses.RespondWithStatusCodeOnly(w, http.StatusCreated)
}
