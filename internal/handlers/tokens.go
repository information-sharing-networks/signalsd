package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/auth"
	"github.com/nickabs/signals/internal/helpers"
)

type AuthHandler struct {
	cfg *signals.ServiceConfig
}

func NewAuthHandler(cfg *signals.ServiceConfig) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

// RefreshAccessTokenHandler returns a new access token in the response body when
// a valid access token is provided as a Authorization Bearer {access token} header.
// Refreshed access tokens are not issued if the token has expired or been revoked.
//
// Note access tokens are 256bit randmom strings, not JWTs.
//
// If the access token has expired, the user must reauthenticate go get a new token.
func (a *AuthHandler) RefreshAccessTokenHandler(w http.ResponseWriter, r *http.Request) {
	type refreshResponse struct {
		AccessToken string `json:"token"`
	}

	authService := auth.NewAuthService(a.cfg)

	if r.ContentLength != 0 {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintln("This endpoint does not expect a request body"))
		return
	}

	refreshToken, err := authService.BearerTokenFromHeader(r.Header)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeTokenError, fmt.Sprintf("could not refresh token: %v", err))
		return
	}

	refreshTokenRow, err := a.cfg.DB.GetRefreshToken(r.Context(), refreshToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeTokenError, "Invalid token")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeTokenError, "could not refresh the token")
		return
	}
	if refreshTokenRow.ExpiresAt.In(time.UTC).Before(time.Now().In(time.UTC)) {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeRefreshTokenExpired, "the supplied token has expired - please login again ")
		return
	}
	if refreshTokenRow.RevokedAt.Valid {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeRefreshTokenRevoked, "the supplied token was revoked previously")
		return
	}

	accessToken, err := authService.GenerateAccessToken(refreshTokenRow.UserID, a.cfg.SecretKey, signals.AccessTokenExpiry)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "could not generate access token")
		return
	}

	res := refreshResponse{
		AccessToken: accessToken,
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)
}

// RevokeRefreshTokenHandler revokes the refresh token supplied in the Authorization header
func (a *AuthHandler) RevokeRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength != 0 {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintln("This endpoint does not expect a request body"))
		return
	}

	authService := auth.NewAuthService(a.cfg)

	refreshToken, err := authService.BearerTokenFromHeader(r.Header)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeTokenError, fmt.Sprintf("could not revoke token: %v", err))
		return
	}

	rowsAffected, err := a.cfg.DB.RevokeRefreshToken(r.Context(), refreshToken)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeTokenError, fmt.Sprintf("error getting token from database: %v", err))
		return
	}
	if rowsAffected == 0 {
		helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeTokenError, "refresh token not found")
		return
	}
	if rowsAffected != 1 {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "database error")
		return
	}

	helpers.RespondWithJSON(w, http.StatusNoContent, "")

}
