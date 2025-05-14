package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/auth"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
)

type UserHandler struct {
	cfg *signals.ServiceConfig
}

func NewUserHandler(cfg *signals.ServiceConfig) *UserHandler {
	return &UserHandler{cfg: cfg}
}

type CreateUserRequest struct {
	Password string `json:"password" example:"lkIB53@6O^Y"` // passwords must be at least 11 chars long
	Email    string `json:"email" example:"example@example.com"`
}

type CreateUserResponse struct {
	ID          uuid.UUID `json:"id" example:"68fb5f5b-e3f5-4a96-8d35-cd2203a06f73"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/users/01a38b82-cbc7-4a24-b61f-e55cb99ac41e"`
}

// todo forgotten password
type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password" example:"lkIB53@6O^Y"`
	NewPassword     string `json:"new_password" example:"ue6U>&X3j570"`
}

// CreateUserHandler godoc
//
//	@Summary	Create user
//	@Tags		auth
//
//	@Param		request	body		handlers.UpdatePasswordRequest	true	"user details"
//
//	@Success	201		{object}	handlers.CreateUserResponse
//	@Failure	400		{object}	signals.ErrorResponse
//	@Failure	409		{object}	signals.ErrorResponse
//	@Failure	500		{object}	signals.ErrorResponse
//
//	@Router		/auth/register [post]
func (u *UserHandler) CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest

	var newUser = database.User{}

	authService := auth.NewAuthService(u.cfg)

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.Email == "" || req.Password == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "you must supply {email} and {password}")
		return
	}

	exists, err := u.cfg.DB.ExistsUserWithEmail(r.Context(), req.Email)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		helpers.RespondWithError(w, r, http.StatusConflict, signals.ErrCodeUserAlreadyExists, "a user already exists this email address")
		return
	}

	if len(req.Password) < signals.MinimumPasswordLength {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodePasswordTooShort, fmt.Sprintf("password must be at least %d chars", signals.MinimumPasswordLength))
		return
	}

	hashedPassword, err := authService.HashPassword(req.Password)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("could not hash password: %v", err))
		return
	}

	newUser, err = u.cfg.DB.CreateUser(r.Context(), database.CreateUserParams{
		HashedPassword: hashedPassword,
		Email:          req.Email,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not create user: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/users/%s",
		helpers.GetScheme(r),
		r.Host,
		newUser.ID,
	)

	helpers.RespondWithJSON(w, http.StatusCreated, CreateUserResponse{
		ID:          newUser.ID,
		ResourceURL: resourceURL,
	})
}

// UpdatePasswordHandler godoc
//
//	@Summary		Update password
//	@Description	Use this api to reset the users password.  Requires a valid access token and the current password
//	@Description	TODO - forgotten password facility
//	@Tags			auth
//
//	@Param			request	body	handlers.UpdatePasswordRequest	true	"user details"
//	@Param			id	path	string								true	"user id"	example(sample-ISN--example-org)
//	@Success		204
//	@Failure		400	{object}	signals.ErrorResponse
//	@Failure		401	{object}	signals.ErrorResponse
//	@Failure		404	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/auth/password/reset [put]
func (u *UserHandler) UpdatePasswordHandler(w http.ResponseWriter, r *http.Request) {
	req := UpdatePasswordRequest{}
	authService := auth.NewAuthService(u.cfg)

	ctx := r.Context()

	// this request was already authenticated by the middleware
	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.NewPassword == "" || req.CurrentPassword == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "you must supply current and new password in the request")
		return
	}

	user, err := u.cfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("database error: %v", userID))
		return
	}

	currentPasswordHash, err := authService.HashPassword(req.CurrentPassword)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("server error: %v", err))
		return
	}

	err = authService.CheckPasswordHash(currentPasswordHash, req.CurrentPassword)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeAuthenticationFailure, "Incorrect email or password")
		return
	}

	if len(req.NewPassword) < signals.MinimumPasswordLength {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodePasswordTooShort, fmt.Sprintf("password must be at least %d chars", signals.MinimumPasswordLength))
		return
	}

	newPasswordHash, err := authService.HashPassword(req.NewPassword)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("server error: %v", err))
		return
	}

	rowsAffected, err := u.cfg.DB.UpdatePassword(r.Context(), database.UpdatePasswordParams{
		ID:             user.ID,
		HashedPassword: newPasswordHash,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if rowsAffected != 1 {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "error updating user")
		return
	}

	helpers.RespondWithJSON(w, http.StatusNoContent, "")

}

// todo query by email (admin only)
// GetUserbyIDHandler godoc
//
//	@Summary	Get registered user
//	@Tags		auth
//
//	@Param		id	path		string	true	"user id"	example(68fb5f5b-e3f5-4a96-8d35-cd2203a06f73)
//	@Success	200	{array}		database.GetForDisplayUserByIDRow
//	@Failure	500	{object}	signals.ErrorResponse
//
//	@Router		/api/users/{id} [get]
func (u *UserHandler) GetUserByIDHandler(w http.ResponseWriter, r *http.Request) {

	userIDstring := r.PathValue("id")
	userID, err := uuid.Parse(userIDstring)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, fmt.Sprintf("Invalid user ID: %v", err))
		return
	}

	res, err := u.cfg.DB.GetForDisplayUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeResourceNotFound, fmt.Sprintf("No user found for id %v", userID))
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user from the database %v", err))
		return
	}
	//
	helpers.RespondWithJSON(w, http.StatusOK, res)
}
