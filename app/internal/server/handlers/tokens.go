package handlers

import (
	"fmt"
	"net/http"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/server/responses"

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
//	@Description	A vaild refresh token is needed to use this endpoint - if the refresh token has expired or been revoked the user must login again to get a new one.
//	@Description	New refresh tokens are sent as http-only cookies whenever the client uses this endpoint or logs in.
//	@Description
//	@Description	The browser handles refresh token cookies automatically, but you must provide your current access token in the Authorization header (the token is used only for user identification - expired tokens are accepted).
//	@Description
//	@Description	Each successful refresh operation:
//	@Description	- Invalidates the current refresh token
//	@Description	- Issues a new refresh token via secure HTTP-only cookie
//	@Description	- Returns a new access token in the response body
//	@Description
//	@Description	the new refresh token cookies is configured with:
//	@Description	- HttpOnly - Prevents JavaScript access, reducing XSS risk
//	@Description	- Secure - HTTPS (only in production environments)
//	@Description	- SameSite=Lax -  preventing the use of cookie in third-party requests except for user-driven interactions.
//	@Description
//	@Description	The account's role and permissions are encoded and included as part of the jwt access token and also provided in the response body
//	@Description
//	@Description	Access tokens expire after 30 mins and subsequent requests using the token will fail with HTTP status 401 and an error_code of "access_token_expired"
//	@Tags			auth
//
//	@Success		200	{object}	auth.AccessTokenResponse
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerRefreshToken
//
//	@Router			/auth/token [post]
func (a *TokenHandler) RefreshAccessTokenHandler(w http.ResponseWriter, r *http.Request) {

	// the RequireValidRefreshToken middleware adds the userAccountId
	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	accessTokenResponse, err := a.authService.BuildAccessTokenResponse(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("error creating access token: %v", err))
		return
	}

	newRefreshToken, err := a.authService.RotateRefreshToken(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("error creating refresh token: %v", err))
		return
	}

	newCookie := a.authService.NewRefreshTokenCookie(a.environment, newRefreshToken)

	http.SetCookie(w, newCookie)

	log.Info().Msgf("user %v refreshed an access token", userAccountID)

	responses.RespondWithJSON(w, http.StatusOK, accessTokenResponse)
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
//	@Tags		auth
//
//	@Success	204
//	@Failure	400	{object}	responses.ErrorResponse
//	@Failure	404	{object}	responses.ErrorResponse
//	@Failure	500	{object}	responses.ErrorResponse
//
//	@Security	BearerRefreshToken
//
//	@Router		/auth/revoke [post]
//
// RevokeRefreshTokenHandler gets the request from the RequireValidRefreshToken middleware
// The middleware identifies the user and confirms there is a valid refresh token in the refresh_token cookie
// and - if there is - adds the hashed token to the auth.AuthContext This function marks the token as revoked on the database.
func (a *TokenHandler) RevokeRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {

	userAccountId, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a userAccountID")
		return
	}
	hashedRefreshToken, ok := auth.ContextHashedRefreshToken(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a refresh token")
		return
	}

	rowsAffected, err := a.queries.RevokeRefreshToken(r.Context(), hashedRefreshToken)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting token from database: %v", err))
		return
	}
	if rowsAffected == 0 {
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeTokenInvalid, "refresh token not found")
		return
	}
	if rowsAffected != 1 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	log.Info().Msgf("refresh token revoked by userAccountID %v", userAccountId)
	responses.RespondWithStatusCodeOnly(w, http.StatusCreated)

}
