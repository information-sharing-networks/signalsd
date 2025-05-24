package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/response"

	signalsd "github.com/nickabs/signalsd/app"
)

type UserHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
	db          *sql.DB
}

func NewUserHandler(queries *database.Queries, authService *auth.AuthService, db *sql.DB) *UserHandler {
	return &UserHandler{
		queries:     queries,
		authService: authService,
		db:          db,
	}
}

type CreateUserRequest struct {
	Password string `json:"password" example:"lkIB53@6O^Y"` // passwords must be at least 11 characters long
	Email    string `json:"email" example:"example@example.com"`
}

// todo forgotten password
type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password" example:"lkIB53@6O^Y"`
	NewPassword     string `json:"new_password" example:"ue6U>&X3j570"`
}

// RegisterUserHandler godoc
//
//	@Summary		Create user
//	@Tags			auth
//
//	@Param			request	body	handlers.CreateUserRequest	true	"user details"
//	@Description	The first user to be created for this service will be created with an admin role.
//	@Description	Subsequent accounts default to standard user roles.
//
//	@Success		201
//	@Failure		400	{object}	response.ErrorResponse
//	@Failure		409	{object}	response.ErrorResponse
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Router			/auth/register [post]
func (u *UserHandler) RegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.Email == "" || req.Password == "" {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply {email} and {password}")
		return
	}

	exists, err := u.queries.ExistsUserWithEmail(r.Context(), req.Email)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		response.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeUserAlreadyExists, "a user already exists this email address")
		return
	}

	if len(req.Password) < signalsd.MinimumPasswordLength {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodePasswordTooShort, fmt.Sprintf("password must be at least %d chars", signalsd.MinimumPasswordLength))
		return
	}

	hashedPassword, err := u.authService.HashPassword(req.Password)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not hash password: %v", err))
		return
	}

	// create the account record followed by the user (note transaction needed to ensure both records are created together)
	tx, err := u.db.BeginTx(r.Context(), nil)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to being transaction: %v", err))
		return
	}

	defer tx.Rollback()

	txQueries := u.queries.WithTx(tx)

	account, err := txQueries.CreateUserAccount(r.Context())
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert account record: %v", err))
		return
	}

	// first user is granted the owner role
	isFirstUser, err := u.queries.IsFirstUser(r.Context())
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
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
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create user: %v", err))
		return
	}

	if err := tx.Commit(); err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}

	response.RespondWithJSON(w, http.StatusCreated, "")
}

// UpdatePasswordHandler godoc
//
//	@Summary		Reset password
//	@Description	Use this api to reset the user's password.  Requires a valid access token and the current password
//	@Description
//	@Description	TODO - forgotten password facility
//	@Tags			auth
//
//	@Param			request	body	handlers.UpdatePasswordRequest	true	"user details"
//	@Success		204
//	@Failure		400	{object}	response.ErrorResponse
//	@Failure		401	{object}	response.ErrorResponse
//	@Failure		404	{object}	response.ErrorResponse
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/auth/password/reset [put]
func (u *UserHandler) UpdatePasswordHandler(w http.ResponseWriter, r *http.Request) {
	req := UpdatePasswordRequest{}

	// this request was already authenticated by the middleware
	userAccountID, ok := auth.ContextUserAccountID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.NewPassword == "" || req.CurrentPassword == "" {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply current and new password in the request")
		return
	}

	user, err := u.queries.GetUserByID(r.Context(), userAccountID)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error retreiving user from access code (%v) %v", userAccountID, err))
		return
	}

	currentPasswordHash, err := u.authService.HashPassword(req.CurrentPassword)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("server error: %v", err))
		return
	}

	err = u.authService.CheckPasswordHash(currentPasswordHash, req.CurrentPassword)
	if err != nil {
		response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "Incorrect email or password")
		return
	}

	if len(req.NewPassword) < signalsd.MinimumPasswordLength {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodePasswordTooShort, fmt.Sprintf("password must be at least %d chars", signalsd.MinimumPasswordLength))
		return
	}

	newPasswordHash, err := u.authService.HashPassword(req.NewPassword)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("server error: %v", err))
		return
	}

	rowsAffected, err := u.queries.UpdatePassword(r.Context(), database.UpdatePasswordParams{
		AccountID:      user.AccountID,
		HashedPassword: newPasswordHash,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if rowsAffected != 1 {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "error updating user")
		return
	}

	response.RespondWithJSON(w, http.StatusNoContent, "")

}

// GetUserbyIDHandler godoc
//
//	@Summary		Get registered user
//	@Description	This API is protected (includes email addresses in the response) - currently only available on dev envs, pending implementation of admin roles.
//	@Tags			auth
//
//	@Param			id	path		string	true	"user id"	example(68fb5f5b-e3f5-4a96-8d35-cd2203a06f73)
//	@Success		200	{array}		database.GetUserByIDRow
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Router			/admin/users/{id} [get]
func (u *UserHandler) GetUserHandler(w http.ResponseWriter, r *http.Request) {

	userAccountIDstring := r.PathValue("id")
	userAccountID, err := uuid.Parse(userAccountIDstring)
	if err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid user ID: %v", err))
		return
	}

	res, err := u.queries.GetUserByID(r.Context(), userAccountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No user found for id %v", userAccountID))
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user from the database %v", err))
		return
	}
	//
	response.RespondWithJSON(w, http.StatusOK, res)
}

// GetUsersHandler godoc
//
//	@Summary		Get the registered users
//	@Description	This api displays email addresses and is currently only available on dev env pending implementation of role based access
//	@Tags			auth
//
//	@Success		200	{array}		database.GetUsersRow
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Router			/api/users [get]
func (u *UserHandler) GetUsersHandler(w http.ResponseWriter, r *http.Request) {

	res, err := u.queries.GetUsers(r.Context())
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting user from database: %v", err))
		return
	}
	response.RespondWithJSON(w, http.StatusOK, res)
}
