package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/auth"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
)

type LoginHandler struct {
	cfg *signals.ServiceConfig
}

func NewLoginHandler(cfg *signals.ServiceConfig) *LoginHandler {
	return &LoginHandler{cfg: cfg}
}
func (l *LoginHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	type loginRequest struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}

	type loginResponse struct {
		ID           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
	}

	var req loginRequest

	authService := auth.NewAuthService(l.cfg)

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}
	defer r.Body.Close()

	exists, err := l.cfg.DB.ExistsUserWithEmail(r.Context(), req.Email)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if !exists {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeUserNotFound, "no user found with this email address")
		return
	}

	user, err := l.cfg.DB.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	err = authService.CheckPasswordHash(user.HashedPassword, req.Password)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeAuthenticationFailure, "Incorrect email or password")
		return
	}

	accessToken, err := authService.GenerateAccessToken(user.ID, l.cfg.SecretKey, signals.AccessTokenExpiry)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeTokenError, fmt.Sprintf("error creating access token: %v", err))
		return
	}

	refreshToken, err := authService.GenerateRefreshToken()
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeTokenError, fmt.Sprintf("error creating refresh token: %v", err))
		return
	}

	_, err = l.cfg.DB.InsertRefreshToken(r.Context(), database.InsertRefreshTokenParams{
		Token:     refreshToken,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(signals.RefreshTokenExpiry),
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not create user: %v", err))
		return
	}

	res := loginResponse{
		ID:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)
}
