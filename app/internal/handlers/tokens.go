package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	signalsd "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/context"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/response"
)

type TokenHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
}

func NewTokenHandler(queries *database.Queries, authService *auth.AuthService) *TokenHandler {
	return &TokenHandler{
		queries:     queries,
		authService: authService,
	}
}

// RefreshAccessTokenHandler godoc
//
//	@Summary		Refresh access token
//	@Description	Use this endpoint to get a new access token.
//	@Description	Access tokens expire after an hour and subsequent requests using the token will fail with an error_code of "access_token_expired"
//	@Description
//	@Description	You need to supply a vaild refresh_token to use this API.
//	@Description	If the refresh token has expired ("refresh_token_expired") or been revoked ("refresh_token_revoked") the user must login again to get a new one.
//	@Tags			auth
//
//	@Success		200	{object}	handlers.RefreshAccessTokenHandler.refreshResponse
//	@Failure		400	{object}	response.ErrorResponse
//	@Failure		401	{object}	response.ErrorResponse
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Security		BearerRefreshToken
//
//	@Router			/auth/refresh-token [post]
//
// note refresh tokens are random 256b strings, not JWTs
func (a *TokenHandler) RefreshAccessTokenHandler(w http.ResponseWriter, r *http.Request) {

	type refreshResponse struct {
		AccessToken string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJTaWduYWxTZXJ2ZXIiLCJzdWIiOiI2OGZiNWY1Yi1lM2Y1LTRhOTYtOGQzNS1jZDIyMDNhMDZmNzMiLCJleHAiOjE3NDY3NzA2MzQsImlhdCI6MTc0Njc2NzAzNH0.3OdnUNgrvt1Zxs9AlLeaC9DVT6Xwc6uGvFQHb6nDfZs"`
	}

	userAccountID, ok := context.UserAccountID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	accessToken, err := a.authService.GenerateAccessToken(userAccountID, signalsd.AccessTokenExpiry)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not generate access token")
		return
	}

	res := refreshResponse{
		AccessToken: accessToken,
	}
	response.RespondWithJSON(w, http.StatusOK, res)
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
//	@Failure		400	{object}	response.ErrorResponse
//	@Failure		404	{object}	response.ErrorResponse
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Router			/auth/revoke-refresh-token [post]
func (a *TokenHandler) RevokeRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	type revokeRefreshTokenRequest struct {
		RefreshToken string `json:"refresh_token" example:"fb948e0b74de1f65e801b4e70fc9c047424ab775f2b4dc5226f472f3b6460c37"`
	}
	var req revokeRefreshTokenRequest

	// todo client authorization?

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not decode request body: %v", err))
		return
	}
	if req.RefreshToken == "" {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply a refresh token in the body of the request")
		return
	}

	rowsAffected, err := a.queries.RevokeRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("error getting token from database: %v", err))
		return
	}
	if rowsAffected == 0 {
		response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeTokenError, "refresh token not found")
		return
	}
	if rowsAffected != 1 {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	response.RespondWithJSON(w, http.StatusNoContent, "")

}
