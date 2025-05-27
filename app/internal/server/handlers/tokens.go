package handlers

import (
	"fmt"
	"net/http"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"

	"github.com/nickabs/signalsd/app/internal/utils"
	"github.com/rs/zerolog/log"
)

type TokenHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
	environment string
}

func NewTokenHandler(queries *database.Queries, authService *auth.AuthService, environment string) *TokenHandler {
	return &TokenHandler{
		queries:     queries,
		authService: authService,
		environment: environment,
	}
}

// RefreshAccessTokenHandler godoc
//
//	@Summary		Refresh access token
//	@Description	Use this endpoint to get a new access token.
//	@Description
//	@Description	You need to supply a vaild refresh token to use this API - if the refresh token has expired or been revoked the user must login again to get a new one.
//	@Description
//	@Description	The refresh token should be supplied in a http-only cookie called refresh_token.
//	@Description
//	@Description	You must also provide a previously issued bearer access token - it does not matter if it has expired
//	@Description	(the token is not used to authenticate the request but is needed to establish the ID of the user making the request)
//	@Description
//	@Description	Note this action automatically revokes the current refresh_token and issues a new one.
//	@Description
//	@Description	The new refresh token is sent in an http-only cookie named refresh_token.
//	@Description	In production deployments the secure flag (https only) on the cookie will be set to true
//	@Description
//	@Description	Access tokens expire after 30 mins and subsequent requests using the token will fail with an error_code of "access_token_expired"
//	@Description
//
//	@Tags			auth
//
//	@Success		200	{object}	auth.AccessTokenResponse
//	@Failure		400	{object}	utils.ErrorResponse
//	@Failure		401	{object}	utils.ErrorResponse
//	@Failure		500	{object}	utils.ErrorResponse
//
//	@Security		BearerRefreshToken
//
//	@Router			/auth/token [post]
func (a *TokenHandler) RefreshAccessTokenHandler(w http.ResponseWriter, r *http.Request) {

	// the RequireValidRefreshToken middleware adds the userAccountId
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	accessTokenResponse, err := a.authService.BuildAccessTokenResponse(r.Context())
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("error creating access token: %v", err))
		return
	}

	newRefreshToken, err := a.authService.RotateRefreshToken(r.Context())
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("error creating refresh token: %v", err))
		return
	}

	newCookie := a.authService.NewRefreshTokenCookie(a.environment, newRefreshToken)

	http.SetCookie(w, newCookie)

	log.Info().Msgf("user %v refreshed an access token", userAccountID)

	utils.RespondWithJSON(w, http.StatusOK, accessTokenResponse)
}

// RevokeRefreshTokenHandler godoc
//
//	@Summary		Revoke refresh token
//	@Description	Revoke a refresh token to prevent it being used to create new access tokens.
//	@Description
//	@Description	You need to supply a vaild refresh token to use this API - if the refresh token has expired or been revoked the user must login again to get a new one.
//	@Description
//	@Description	The refresh token should be supplied in a http-only cookie called refresh_token.
//	@Description
//	@Description	You must also provide a previously issued bearer access token - it does not matter if it has expired
//	@Description	(the token is not used to authenticate the request but is needed to establish the ID of the user making the request)
//	@Description
//	@Description	Note that any unexpired access tokens issued for this user will continue to work until they expire.
//	@Description	Users must log in again to obtain a new refresh token if the current one has been revoked.
//	@Description
//	@Tags			auth
//
//	@Success		204
//	@Failure		400	{object}	utils.ErrorResponse
//	@Failure		404	{object}	utils.ErrorResponse
//	@Failure		500	{object}	utils.ErrorResponse
//
//	@Security		BearerRefreshToken
//
//	@Router			/auth/revoke [post]
//
// RevokeRefreshTokenHandler gets the request from the RequireValidRefreshToken middleware
// The middleware identifies the user and confirms there is a valid refresh token in the refresh_token cookie
// and - if there is - adds the hashed token to the auth.AuthContext This function marks the token as revoked on the database.
func (a *TokenHandler) RevokeRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {

	userAccountId, ok := auth.ContextAccountID(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a userAccountID")
		return
	}
	hashedRefreshToken, ok := auth.ContextHashedRefreshToken(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a refresh token")
		return
	}

	rowsAffected, err := a.queries.RevokeRefreshToken(r.Context(), hashedRefreshToken)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("error getting token from database: %v", err))
		return
	}
	if rowsAffected == 0 {
		utils.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeTokenError, "refresh token not found")
		return
	}
	if rowsAffected != 1 {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	log.Info().Msgf("refresh token revoked by userAccountID %v", userAccountId)
	utils.RespondWithJSON(w, http.StatusNoContent, "")

}
