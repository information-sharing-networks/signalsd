package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type ServiceAccountTokenRequest struct {
	ClientID     string `json:"client_id" example:"sa_exampleorg_k7j2m9x1"`
	ClientSecret string `json:"client_secret" example:"dGhpcyBpcyBhIHNlY3JldA"`
}

type ServiceAccountRotateResponse struct {
	ClientID     string `json:"client_id" example:"sa_exampleorg_k7j2m9x1"`
	ClientSecret string `json:"client_secret" example:"dGhpcyBpcyBhIHNlY3JldA"`
	ExpiresAt    string `json:"expires_at" example:"2025-07-05T10:30:00Z"`
	ExpiresIn    int    `json:"expires_in" example:"31536000"` // seconds (1 year)
}

type TokenHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
	pool        *pgxpool.Pool
	environment string
}

func NewTokenHandler(queries *database.Queries, authService *auth.AuthService, pool *pgxpool.Pool, environment string) *TokenHandler {
	return &TokenHandler{
		queries:     queries,
		authService: authService,
		pool:        pool,
		environment: environment,
	}
}

// NewAccessTokenHandler godoc
//
//	@Summary	New Access Token
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
//	@Tags		auth
//
//	@Param		grant_type	query		string							true	"grant type"	Enums(client_credentials, refresh_token)
//	@Param		request		body		ServiceAccountTokenRequest	false	"Service account credentials (required for client_credentials grant)"
//
//	@Success	200			{object}	auth.AccessTokenResponse
//	@Failure	400			{object}	responses.ErrorResponse	"Invalid grant_type parameter "
//	@Failure	401			{object}	responses.ErrorResponse	"Authentication failed "
//
//	@Security	BearerAccessToken
//
//	@Router		/oauth/token [post]
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
//	@Summary		Revoke a token
//	@Description	Revoke a refresh token or client secret to prevent it being used to create new access tokens (self-service)
//	@Description
//	@Description	**Use Cases:**
//	@Description	- **Web User Logout**: User wants to log out of their session
//	@Description	- **Service Account Security**: Account no longer being used/compromised secret
//	@Description
//	@Description	**Service Accounts:**
//	@Description	You must supply your `client ID` and `client secret` in the request body.
//	@Description	This revokes **ALL** client secrets for the service account, effectively disabling it.
//	@Description
//	@Description	**IMPORTANT - Service Account Reinstatement:**
//	@Description	- This endpoint does **NOT** permanently disable the service account itself
//	@Description	- To restore access, an admin must call `POST /api/auth/register/service-accounts` with the same organization and email
//	@Description	- This will generate a new setup URL and client secret while preserving the same client_id
//	@Description	- If the account was disabled by an admin, it must first be re-enabled via `POST /admin/accounts/{account_id}/enable`
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

// RevokeClientSecretHandler revokes ALL client secrets for a service account - called by the wrapper handler for /oauth/revoke (RevokeTokenHandler)
// This effectively disables the service account until an admin re-registers it via POST /api/auth/register/service-accounts
func (a *TokenHandler) RevokeClientSecretHandler(w http.ResponseWriter, r *http.Request) {
	serverAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a serverAccountID")
		return
	}

	// Revoke all client secrets for this account (disables the service account)
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

// RevokeRefreshTokenHandler revokes a specific refresh token for web users (logout) - called by the wrapper handler for /oauth/revoke (RevokeTokenHandler)
// Users can log back in immediately via /auth/login to get a new refresh token
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

// RotateServiceAccountSecretHandler godoc
//
//	@Summary		Rotate service account client secret
//	@Description	Self-service endpoint for service accounts to rotate their client secret.
//	@Description	Requires current valid client_id and client_secret for authentication.
//	@Description	The old secret remains valid for 5 minutes to prevent race conditions when multiple instances are involved and to stop clients being locked out where network issues prevent them from receiving the new secret immediately.
//
// y.
//
//	@Description
//	@Description	**Use Cases:**
//	@Description	- Regular credential rotation for security compliance
//	@Description	- Suspected credential compromise requiring immediate rotation
//	@Description
//	@Tags		auth
//
//	@Param		request		body		ServiceAccountTokenRequest	false	"Service account credentials"
//
//	@Success	200	{object}	ServiceAccountRotateResponse
//	@Failure	401	{object}	responses.ErrorResponse	"Authentication failed"
//	@Failure	500	{object}	responses.ErrorResponse	"Internal server error"
//
//	@Router		/api/auth/service-accounts/rotate-secret [post]
func (a *TokenHandler) RotateServiceAccountSecretHandler(w http.ResponseWriter, r *http.Request) {
	logger := zerolog.Ctx(r.Context())

	// Get service account ID from context (set by RequireClientCredentials middleware)
	serviceAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply service account ID")
		return
	}

	// Get service account details to return client_id
	serviceAccount, err := a.queries.GetServiceAccountByAccountID(r.Context(), serviceAccountID)
	if err != nil {
		logger.Error().Err(err).Msgf("failed to get service account: %v", serviceAccountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to get service account details")
		return
	}

	// Generate new client secret
	newPlaintextSecret, err := a.authService.GenerateSecureToken(32)
	if err != nil {
		logger.Error().Err(err).Msg("failed to generate secure token")
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "failed to generate secure token")
		return
	}
	hashedSecret := a.authService.HashToken(newPlaintextSecret)
	expiresAt := time.Now().Add(signalsd.ClientSecretExpiry)

	tx, err := a.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to begin transaction")
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.Error().Err(err).Msg("failed to rollback transaction")
		}
	}()

	txQueries := a.queries.WithTx(tx)

	_, err = txQueries.ScheduleRevokeAllClientSecretsForAccount(r.Context(), serviceAccountID)
	if err != nil {
		logger.Error().Err(err).Msgf("failed to schedule revocation of old secrets for account: %v", serviceAccountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to schedule revocation of old secrets")
		return
	}

	_, err = txQueries.CreateClientSecret(r.Context(), database.CreateClientSecretParams{
		HashedSecret:            hashedSecret,
		ServiceAccountAccountID: serviceAccountID,
		ExpiresAt:               expiresAt,
	})
	if err != nil {
		logger.Error().Err(err).Msgf("failed to create new secret for account: %v", serviceAccountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to create new secret")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to commit transaction")
		return
	}

	logger.Info().Msgf("service account %v rotated client secret (old secret valid for 5 more minutes)", serviceAccount.ClientID)

	response := ServiceAccountRotateResponse{
		ClientID:     serviceAccount.ClientID,
		ClientSecret: newPlaintextSecret,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		ExpiresIn:    int(signalsd.ClientSecretExpiry.Seconds()),
	}

	responses.RespondWithJSON(w, http.StatusOK, response)
}
