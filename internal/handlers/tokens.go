package handlers

import (
	"database/sql"
	"encoding/json"
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
//	@Router			/auth/refresh-token [post]
//
// note refresh tokens are random 256b strings, not JWTs
func (a *AuthHandler) RefreshAccessTokenHandler(w http.ResponseWriter, r *http.Request) {

	type refreshResponse struct {
		AccessToken string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJTaWduYWxTZXJ2ZXIiLCJzdWIiOiI2OGZiNWY1Yi1lM2Y1LTRhOTYtOGQzNS1jZDIyMDNhMDZmNzMiLCJleHAiOjE3NDY3NzA2MzQsImlhdCI6MTc0Njc2NzAzNH0.3OdnUNgrvt1Zxs9AlLeaC9DVT6Xwc6uGvFQHb6nDfZs"`
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
//	@Description
//	@Description	Note that any unexpired access tokens issued for this user will continue to work until they expire.
//	@Description	Users must log in again to obtain a new refresh token if the current one has been revoked.
//	@Description
//	@Description	Anyone in possession of a refresh token can revoke it
//	@Tags			auth
//
//	@Param			request	body	handlers.RevokeRefreshTokenHandler.revokeRefreshTokenRequest	true	"refresh token to be revoked"
//	@Success		204
//	@Failure		400	{object}	signals.ErrorResponse
//	@Failure		404	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Router			/auth/revoke-token [post]
func (a *AuthHandler) RevokeRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	type revokeRefreshTokenRequest struct {
		RefreshToken string `json:"refresh_token" example:"fb948e0b74de1f65e801b4e70fc9c047424ab775f2b4dc5226f472f3b6460c37"`
	}
	var req revokeRefreshTokenRequest

	// todo client authorization?

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("could not decode request body: %v", err))
		return
	}
	if req.RefreshToken == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "you must supply a refresh token in the body of the request")
		return
	}

	rowsAffected, err := a.cfg.DB.RevokeRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeTokenError, fmt.Sprintf("error getting token from database: %v", err))
		return
	}
	if rowsAffected == 0 {
		helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeTokenError, "refresh token not found")
		return
	}
	if rowsAffected != 1 {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	helpers.RespondWithJSON(w, http.StatusNoContent, "")

}
