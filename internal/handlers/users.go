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

type UserLoginDetails struct {
	Password string `json:"password" example:"lkIB53@6O^Y"`
	Email    string `json:"email" example:"example@example.com"`
}

type CreateUserResponse struct {
	ID          uuid.UUID `json:"id" example:"68fb5f5b-e3f5-4a96-8d35-cd2203a06f73"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/users/01a38b82-cbc7-4a24-b61f-e55cb99ac41e"`
}

// CreateUserHandler godoc
//
//	@Summary	Create user
//	@Tags		auth
//
//	@Param		request	body		handlers.UserLoginDetails	true	"user details"
//
//	@Success	201		{object}	handlers.CreateUserResponse
//	@Failure	400		{object}	signals.ErrorResponse
//	@Failure	409		{object}	signals.ErrorResponse
//	@Failure	500		{object}	signals.ErrorResponse
//
//	@Router		/api/users [post]
func (u *UserHandler) CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req UserLoginDetails

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

// UpdateUserHandler godoc
//
//	@Summary		Update user
//	@Description	update email and/or password
//	@Tags			auth
//
//	@Param			request	body	handlers.UserLoginDetails	true	"user details"
//
//	@Success		204
//	@Failure		400	{object}	signals.ErrorResponse
//	@Failure		401	{object}	signals.ErrorResponse
//	@Failure		404	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/users [put]
func (u *UserHandler) UpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	authService := auth.NewAuthService(u.cfg)

	ctx := r.Context()

	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
	}

	defer r.Body.Close()

	req := UserLoginDetails{}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.Email == "" || req.Password == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "expecting a email or password in http body")
		return
	}

	currentUser, err := u.cfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeResourceNotFound, fmt.Sprintf("could not find the user with the id in this access token: %v", userID))
		return
	}

	//prepare update params based on current field values
	updateParams := database.UpdateUserEmailAndPasswordParams{
		ID:             currentUser.ID,
		Email:          currentUser.Email,
		HashedPassword: currentUser.HashedPassword,
	}

	if req.Email != "" {
		updateParams.Email = req.Email
	}

	if req.Password != "" {
		updateParams.HashedPassword, err = authService.HashPassword(req.Password)
		if err != nil {
			helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("server error: %v", err))
			return
		}
	}
	rowsAffected, err := u.cfg.DB.UpdateUserEmailAndPassword(r.Context(), updateParams)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeUserNotFound, "user not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if rowsAffected != 1 {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "error updating user")
		return
	}

	helpers.RespondWithJSON(w, http.StatusNoContent, "")

}

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

// GetUsersHandler godoc
//
//	@Summary	Get the registered users
//	@Tags		auth
//
//	@Success	200	{array}		database.GetForDisplayUsersRow
//	@Failure	500	{object}	signals.ErrorResponse
//
//	@Router		/api/users [get]
func (u *UserHandler) GetUsersHandler(w http.ResponseWriter, r *http.Request) {

	res, err := u.cfg.DB.GetForDisplayUsers(r.Context())
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("error getting user from database: %v", err))
		return
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)
}
