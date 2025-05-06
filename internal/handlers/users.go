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

func (u *UserHandler) CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	type createUserRequest struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}

	var req createUserRequest

	var res database.User

	authService := auth.NewAuthService(u.cfg)

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}
	defer r.Body.Close()

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
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeUserAlreadyExists, "a user already exists this email address")
		return
	}

	hashedPassword, err := authService.HashPassword(req.Password)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("could not hash password: %v", err))
		return
	}

	res, err = u.cfg.DB.CreateUser(r.Context(), database.CreateUserParams{
		HashedPassword: hashedPassword,
		Email:          req.Email,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not create user: %v", err))
		return
	}

	helpers.RespondWithJSON(w, http.StatusCreated, res)
}

// update email or password
func (u *UserHandler) UpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	type updateUserRequest struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	type updateUserResponse struct {
		Email string `json:"email"`
	}

	authService := auth.NewAuthService(u.cfg)

	ctx := r.Context()

	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
	}

	req := updateUserRequest{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}
	defer r.Body.Close()

	if req.Email == "" || req.Password == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "expecting a email or password in http body")
		return
	}

	currentUser, err := u.cfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeResourceNotFound, fmt.Sprintf("could not find a user with the token UUID: %v", userID))
		return
	}

	// prepare update params
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

	helpers.RespondWithJSON(w, http.StatusOK, updateUserResponse{
		Email: updateParams.Email,
	})

}
