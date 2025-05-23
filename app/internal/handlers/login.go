package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/response"

	signalsd "github.com/nickabs/signalsd/app"
)

type LoginHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
}

func NewLoginHandler(queries *database.Queries, authService *auth.AuthService) *LoginHandler {
	return &LoginHandler{
		queries:     queries,
		authService: authService,
	}
}

type LoginRequest struct {
	CreateUserRequest
}

type LoginResponse struct {
	AccountID    uuid.UUID `json:"account_id" example:"68fb5f5b-e3f5-4a96-8d35-cd2203a06f73"`
	CreatedAt    time.Time `json:"created_at" example:"2025-05-09T05:41:22.57328+01:00"`
	AccessToken  string    `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJTaWduYWxTZXJ2ZXIiLCJzdWIiOiI2OGZiNWY1Yi1lM2Y1LTRhOTYtOGQzNS1jZDIyMDNhMDZmNzMiLCJleHAiOjE3NDY3NzA2MzQsImlhdCI6MTc0Njc2NzAzNH0.3OdnUNgrvt1Zxs9AlLeaC9DVT6Xwc6uGvFQHb6nDfZs"`
	RefreshToken string    `json:"refresh_token" example:"fb948e0b74de1f65e801b4e70fc9c047424ab775f2b4dc5226f472f3b6460c37"`
}

// LoginHandler godoc
//
//	@Summary		Login
//	@Description	The response body includes an access token and a refresh_token.
//	@Description	The access_token is valid for 1 hour.
//	@Description
//	@Description	Use the refresh_token with the /auth/refresh-token endpoint to renew the access_token.
//	@Description	The refresh_token lasts 60 days unless it is revoked earlier.
//	@Description	To renew the refresh_token, log in again.
//	@Tags			auth
//
//	@Param			request	body		handlers.LoginRequest	true	"email and password"
//
//	@Success		200		{object}	handlers.LoginResponse
//	@Failure		400		{object}	response.ErrorResponse
//	@Failure		401		{object}	response.ErrorResponse
//	@Failure		500		{object}	response.ErrorResponse
//
//	@Router			/auth/login [post]
func (l *LoginHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	exists, err := l.queries.ExistsUserWithEmail(r.Context(), req.Email)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if !exists {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeUserNotFound, "no user found with this email address")
		return
	}

	user, err := l.queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	err = l.authService.CheckPasswordHash(user.HashedPassword, req.Password)
	if err != nil {
		response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "Incorrect email or password")
		return
	}

	accessToken, err := l.authService.GenerateAccessToken(user.AccountID, signalsd.AccessTokenExpiry)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("error creating access token: %v", err))
		return
	}

	refreshToken, err := l.authService.GenerateRefreshToken()
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("error creating refresh token: %v", err))
		return
	}

	_, err = l.queries.InsertRefreshToken(r.Context(), database.InsertRefreshTokenParams{
		Token:         refreshToken,
		UserAccountID: user.AccountID,
		ExpiresAt:     time.Now().Add(signalsd.RefreshTokenExpiry),
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert refresh token: %v", err))
		return
	}

	response.RespondWithJSON(w, http.StatusOK, LoginResponse{
		AccountID:    user.AccountID,
		CreatedAt:    user.CreatedAt,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}
