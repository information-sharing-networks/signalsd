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

// NewAccessTokenHandler godoc
//
//	@Summary		New Access Token
//	@Description
//	@Description	**Client Credentials Grant (Service Accounts):**
//	@Description
//	@Description	Issues new access token (in response body)
//	@Description
//	@Description	- Set `grant_type=client_credentials` as URL parameter
//	@Description	- Provide `client_id` and `client_secret` in request body
//	@Description	- Must provide current access token in Authorization header (expired tokens accepted)
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
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid grant_type parameter "
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/oauth/token [post]
//
// NewAccessTokenHandler handles requests for both service accounts and web users.
// For web users, a new refresh tokens is sent as http-only cookies whenever the client uses this endpoint.
//
// Use with the RequireOAuthGrantType middleware
// this calls the appropriate authentication middleware for the grant_type (client_credentials or refresh_token)) and adds the authenticated accountID to the context
func (a *TokenHandler) NewAccessTokenHandler(w http.ResponseWriter, r *http.Request) {

	logger := zerolog.Ctx(r.Context())

	// RequireValidRefreshToken / RequireClientCredentials middleware adds the userAccountId or serverAccountAccountID to the context
	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	accountType, ok := auth.ContextAccountType(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive accountType from middleware")
		return
	}

	// todo account is active?

	// create new access token refresh
	accessTokenResponse, err := a.authService.CreateAccessToken(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("error creating access token: %v", err))
		return
	}

	// rotate the refresh token (web users only)
	if accountType == "user" {
		newRefreshToken, err := a.authService.RotateRefreshToken(r.Context())
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("error creating refresh token: %v", err))
			return
		}

		newCookie := a.authService.NewRefreshTokenCookie(a.environment, newRefreshToken)

		http.SetCookie(w, newCookie)
	}
	logger.Info().Msgf("account %v refreshed an access token", accountID)

	responses.RespondWithJSON(w, http.StatusOK, accessTokenResponse)
}

// RevokeTokenHandler godoc
//
//	@Summary		Revoke a token (logout for web users)
//	@Description	Revoke a refresh token or client secret to prevent it being used to create new access tokens.
//	@Description	**This endpoint serves as the logout function for web users.**
//	@Description
//	@Description	**Service Accounts:**
//	@Description 	You must supply your `client ID` and `client secret` in the request body.
//	@Description	This revokes all client secrets for the service account.
//	@Description
//	@Description	**Web Users (Logout):**
//	@Description	This endpoint expects a refresh token in an `http-only cookie` and a valid access token in the Authorization header.
//	@Description	This revokes the user's refresh token, effectively logging them out.
//	@Description
//	@Description	If the refresh token has expired or been revoked, the user must login again to get a new one.
//	@Description
//	@Description	You must also provide a previously issued `bearer access token` in the Authorization header - it does not matter if it has expired
//	@Description	(the token is not used to authenticate the request but is needed to establish the ID of the user making the request).
//	@Description
//	@Description	**Note:** Any unexpired access tokens issued for this user will continue to work until they expire.
//	@Description	Users must log in again to obtain a new refresh token after logout/revocation.
//	@Description
//	@Description	**Client Examples:**
//	@Description	- **Web User Logout:** `POST /oauth/revoke` with refresh token cookie + Authorization header
//	@Description	- **Service Account:** `POST /oauth/revoke` with client_id and client_secret in request body
//	@Description
//	@Tags		auth
//
//	@Success	200
//	@Failure	400	{object}	responses.ErrorResponse	"Invalid request body "
//	@Failure	401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure	404	{object}	responses.ErrorResponse	"Token not found or already revoked"
//	@Failure	500	{object}	responses.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/oauth/revoke [post]
//
// Use with RequireValidAccountTypeCredentials middleware which will ensure the requestor is authenticated and add the accountID and accountType to the context
func (a *TokenHandler) RevokeTokenHandler(w http.ResponseWriter, r *http.Request) {
	accountType, ok := auth.ContextAccountType(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get account type from context")
		return
	}

	if accountType == "user" {
		a.RevokeRefreshTokenHandler(w, r)
		return
	}
	// service accounts
	a.RevokeClientSecretHandler(w, r)
}

// Use revoke a client secret (service accounts) - called by the wrapper handler for /oauth/revoke (RevokeTokenHandler)
func (a *TokenHandler) RevokeClientSecretHandler(w http.ResponseWriter, r *http.Request) {
	serverAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a serverAccountID")
		return
	}

	// cancle all client secrets for this account
	rowsUpdated, err := a.queries.RevokeAllClientSecretsForAccount(r.Context(), serverAccountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error revoking client secrets: %v", err))
		return
	}

	if rowsUpdated == 0 {
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeTokenInvalid, "no client secrets found to revoke")
		return
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusOK)
}

// Use revoke a refresh token (web users) - called by the wrapper handler for /oauth/revoke (RevokeTokenHandler)
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
	responses.RespondWithStatusCodeOnly(w, http.StatusOK)

}
