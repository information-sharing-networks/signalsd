package handlers

import (
	"fmt"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"

	"github.com/rs/zerolog"
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

// TokenHandler godoc
//
//	@Summary		Get Access Token
//	@Description
//	@Description	**Client Credentials Grant (Service Accounts):**
//	@Description
//	@Description	Issues new access token (in response body)
//	@Description
//	@Description	- Set `grant_type=client_credentials` as URL parameter
//	@Description	- Provide `client_id` and `client_secret` in request body
//	@Description	- Access tokens expire after 30 minutes
//	@Description	(subsequent requests using the token will fail with HTTP status 401 and an error_code of "access_token_expired")
//	@Description
//	@Description	**Refresh Token Grant (Web Users):**
//	@Description
//	@Description	Issues new access token (in response body) and rotates refresh token (HTTP-only cookie)
//	@Description
//	@Description	- Set `grant_type=refresh_token` as URL parameter
//	@Description	- Must provide current access token in Authorization header (expired tokens accepted)
//	@Description	- Must have valid refresh token cookie
//	@Description	- Access tokens expire after 30 minutes
//	@Description	(subsequent requests using the token will fail with HTTP status 401 and an error_code of "access_token_expired")
//	@Description	- Refresh tokens expire after 30 days
//	@Description	- subsequent requests using the refresh token will fail with HTTP status 401 and an error_code of "refresh_token_expired" and users must login again to get a new one.
//	@Description
//	@Tags			auth
//
//	@Param			grant_type	query	string	true	"grant type"	Enums(client_credentials, refresh_token)
//	@Param			request		body	auth.ServiceAccountTokenRequest	false	"Service account credentials (required for client_credentials grant)"
//
//	@Success		200	{object}	auth.AccessTokenResponse
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/oauth/token [post]
func (t *TokenHandler) TokenHandler(w http.ResponseWriter, r *http.Request) {
	grantType := r.URL.Query().Get("grant_type")

	switch grantType {
	case "client_credentials":
		t.ClientCredentialsHandler(w, r)
	case "refresh_token":
		t.RefreshAccessTokenHandler(w, r)
	default:
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("unsupported grant_type: %s", grantType))
	}
}

// RefreshAccessTokenHandler godoc
// for web users
// A valid refresh token is needed to use this endpoint - if the refresh token has expired or been revoked the user must login again to get a new one.
// New refresh tokens are sent as http-only cookies whenever the client uses this endpoint or logs in.
func (a *TokenHandler) RefreshAccessTokenHandler(w http.ResponseWriter, r *http.Request) {

	logger := zerolog.Ctx(r.Context())

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

	logger.Info().Msgf("user %v refreshed an access token", userAccountID)

	responses.RespondWithJSON(w, http.StatusOK, accessTokenResponse)
}

// ClientCredentialsHandler godoc
// for service accounts
// gets request via the RequireValidClientCredentials middleware (this adds the server account account ID to the context)
func (t *TokenHandler) ClientCredentialsHandler(w http.ResponseWriter, r *http.Request) {

	logger := zerolog.Ctx(r.Context())

	serverAccountAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a Server Account ID")
		return
	}

	accessTokenResponse, err := t.authService.BuildAccessTokenResponse(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("error creating access token: %v", err))
		return
	}

	logger.Info().Msgf("serviceAccount %v refreshed an access token", serverAccountAccountID)

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
