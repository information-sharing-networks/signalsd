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

// RefreshAccessTokenHandler godoc
//
//	@Summary		Refresh access token
//	@Description	Returns a new JWT access token.
//	@Description	Access tokens are not issued if the refresh token has expired or been revoked.
//	@Description	Users must log in again to obtain a new refresh token if the current one has expired or been revoked.
//	@Tags			auth
//
//	@Success		200	{object}	handlers.RefreshAccessTokenHandler.refreshResponse
//	@Failure		400	{object}	signals.ErrorResponse
//	@Failure		401	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Security		BearerRefreshToken
//
//	@Router			/api/refresh [post]
//
// note refresh tokens are random 256b strings, not JWTs
func (a *AuthHandler) RefreshAccessTokenHandler(w http.ResponseWriter, r *http.Request) {

	type refreshResponse struct {
		AccessToken string `json:"access_token"`
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
			helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeTokenError, "Invalid token")
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

// RevokeRefreshTokenHandler godoc
//
//	@Summary		Revoke refresh token
//	@Description	Revoke a refresh token to prevent it being used to create new access tokens.
//	@Description	Note that any unexpired access tokens issued for this user will continue to work until they expire.
//	@Description	Users must log in again to obtain a new refresh token if the current one has been revoked.
//	@Tags			auth
//
//	@Success		204
//	@Failure		400	{object}	signals.ErrorResponse
//	@Failure		401	{object}	signals.ErrorResponse
//	@Failure		404	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Security		BearerRefreshToken
//
//	@Router			/api/revoke [post]
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
