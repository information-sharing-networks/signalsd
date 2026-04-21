package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
)

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

// RefreshAccessToken godoc
//
//	@Summary	Refresh Access Token
//	@Description
//	@Description	Issues a new access token. The request body must be `application/x-www-form-urlencoded` (RFC 6749).
//	@Description
//	@Description	---
//	@Description
//	@Description	**Service Accounts — `client_credentials` grant**
//	@Description
//	@Description	Include in the request body:
//	@Description	- `grant_type=client_credentials`
//	@Description	- `client_id=<your_client_id>`
//	@Description	- `client_secret=<your_client_secret>`
//	@Description
//	@Description	Alternatively, credentials may be supplied via HTTP Basic Auth (`Authorization: Basic base64(client_id:client_secret)`).
//	@Description
//	@Description	Returns a new access token in the response body. Access tokens expire after 30 minutes.
//	@Description
//	@Description	---
//	@Description
//	@Description	**Web Users — `refresh_token` grant**
//	@Description
//	@Description	Include in the request body:
//	@Description	- `grant_type=refresh_token`
//	@Description
//	@Description	The refresh token is read automatically from the `refresh_token` HTTP-only cookie set during login.
//	@Description	No additional form fields are required.
//	@Description
//	@Description	Returns a new access token in the response body. The rotated refresh token is set as a new HTTP-only cookie.
//	@Description	Access tokens expire after 30 minutes. Refresh tokens expire after 30 days — if the refresh token
//	@Description	has expired the user must log in again.
//	@Description
//	@Tags		auth
//	@Accept		x-www-form-urlencoded
//
//	@Param		grant_type		formData	string	true	"Grant type"	Enums(client_credentials, refresh_token)
//	@Param		client_id		formData	string	false	"Client ID (client_credentials grant only)"
//	@Param		client_secret	formData	string	false	"Client secret (client_credentials grant only)"
//
//	@Success	200				{object}	auth.AccessTokenResponse
//	@Failure	400				{object}	responses.ErrorResponse	"Missing or invalid grant_type"
//	@Failure	401				{object}	responses.ErrorResponse	"Invalid credentials or expired refresh token"
//
//	@Router		/oauth/token [post]
func (a *TokenHandler) RefreshAccessToken(w http.ResponseWriter, r *http.Request) {

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

	// create new access token refresh
	accessTokenResponse, err := a.authService.CreateAccessToken(r.Context())
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, "error creating access token")
		return
	}

	// rotate the refresh token (web users only)
	if accountType == "user" {
		newRefreshToken, err := a.authService.RotateRefreshToken(r.Context())
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, "error creating refresh token")
			return
		}

		reqLogger := logger.ContextRequestLogger(r.Context())

		reqLogger.Debug("Created new access token",
			slog.String("component", "RefreshAccessTokenHandler"),
			slog.String("account_id", accountID.String()),
			slog.String("account_type", accountType),
		)

		newCookie := a.authService.NewRefreshTokenCookie(newRefreshToken)

		http.SetCookie(w, newCookie)
	}

	responses.RespondWithJSON(w, http.StatusOK, accessTokenResponse)
}

// RevokeToken godoc
//
//	@Summary	Revoke Token
//	@Description
//	@Description	Revokes credentials to prevent them being used to obtain new access tokens.
//	@Description	The request body must be `application/x-www-form-urlencoded` (RFC 6749).
//	@Description	Any access tokens already issued remain valid until they expire (30 minutes).
//	@Description
//	@Description	---
//	@Description
//	@Description	**Service Accounts — `client_credentials` grant (logout / credential rotation)**
//	@Description
//	@Description	Include in the request body:
//	@Description	- `grant_type=client_credentials`
//	@Description	- `client_id=<your_client_id>`
//	@Description	- `client_secret=<your_client_secret>`
//	@Description
//	@Description	Alternatively, credentials may be supplied via HTTP Basic Auth (`Authorization: Basic base64(client_id:client_secret)`).
//	@Description
//	@Description	Revokes **all** client secrets for the service account, which prevents any further token requests
//	@Description	until an admin reissues credentials via `POST /api/auth/service-accounts/reissue_credentials`.
//	@Description	This does not permanently disable the account — use `POST /admin/accounts/{account_id}/disable` for that.
//	@Description
//	@Description	---
//	@Description
//	@Description	**Web Users — `refresh_token` grant (logout)**
//	@Description
//	@Description	Include in the request body:
//	@Description	- `grant_type=refresh_token`
//	@Description
//	@Description	The refresh token is read automatically from the `refresh_token` HTTP-only cookie.
//	@Description	No additional form fields are required.
//	@Description
//	@Description	Revokes the current refresh token. The user must log in again to obtain a new one.
//	@Description
//	@Tags		auth
//	@Accept		x-www-form-urlencoded
//
//	@Param		grant_type		formData	string	true	"Grant type"	Enums(client_credentials, refresh_token)
//	@Param		client_id		formData	string	false	"Client ID (client_credentials grant only)"
//	@Param		client_secret	formData	string	false	"Client secret (client_credentials grant only)"
//
//	@Success	200
//	@Failure	400	{object}	responses.ErrorResponse	"Missing or invalid grant_type"
//	@Failure	401	{object}	responses.ErrorResponse	"Invalid credentials or missing refresh token cookie"
//	@Failure	404	{object}	responses.ErrorResponse	"Token not found or already revoked"
//
//	@Router		/oauth/revoke [post]
func (a *TokenHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	accountType, ok := auth.ContextAccountType(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get account type from context")
		return
	}

	if accountType == "user" {
		a.RevokeRefreshToken(w, r)
		return
	}
	// service accounts
	a.RevokeClientSecret(w, r)
}

// RevokeClientSecret revokes ALL client secrets for a service account - called by the wrapper handler for /oauth/revoke (RevokeTokenHandler)
// This effectively disables the service account until an admin re-registers it via POST /api/auth/service-accounts/register
func (a *TokenHandler) RevokeClientSecret(w http.ResponseWriter, r *http.Request) {

	serverAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply a serverAccountID")
		return
	}

	// Revoke all client secrets for this account (disables the service account)
	rowsUpdated, err := a.queries.RevokeAllClientSecretsForAccount(r.Context(), serverAccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if rowsUpdated == 0 {
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeTokenInvalid, "no client secrets found to revoke")
		return
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusOK)
}

// RevokeRefreshToken revokes a specific refresh token for web users (logout) - called by the wrapper handler for /oauth/revoke (RevokeTokenHandler)
// Users can log back in immediately via /auth/login to get a new refresh token
func (a *TokenHandler) RevokeRefreshToken(w http.ResponseWriter, r *http.Request) {

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

	reqLogger := logger.ContextRequestLogger(r.Context())

	rowsAffected, err := a.queries.RevokeRefreshToken(r.Context(), hashedRefreshToken)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	if rowsAffected == 0 {
		reqLogger.Debug("Error revoking refresh token",
			slog.String("component", "RevokeRefreshTokenHandler"),
			slog.String("account_id", userAccountId.String()),
			slog.String("error", "refresh token not found"),
		)

		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeTokenInvalid, "refresh token not found")
		return
	}
	if rowsAffected != 1 {
		logger.ContextWithLogAttrs(r.Context(),
			slog.Int64("error_rows_affected", rowsAffected),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// add log attributes for final request log
	logger.ContextWithLogAttrs(r.Context(),
		slog.String("component", "RevokeRefreshTokenHandler"),
		slog.String("account_id", userAccountId.String()),
	)
	responses.RespondWithStatusCodeOnly(w, http.StatusOK)

}

// RotateServiceAccountSecret godoc
//
//	@Summary		Rotate Service Account Client Secret
//	@Description	Self-service endpoint for service accounts to rotate their client secret.
//	@Description	This endpoint requires current valid client_id and client_secret for authentication.
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
//	@Accept		x-www-form-urlencoded
//
//	@Param		client_id		formData	string	true	"Client ID"
//	@Param		client_secret	formData	string	true	"Client secret"
//
//	@Success	200				{object}	ServiceAccountRotateResponse
//	@Failure	401				{object}	responses.ErrorResponse	"Authentication failed"
//	@Failure	500				{object}	responses.ErrorResponse	"Internal server error"
//
//	@Router		/api/auth/service-accounts/rotate-secret [post]
func (a *TokenHandler) RotateServiceAccountSecret(w http.ResponseWriter, r *http.Request) {

	// Get service account ID from context (set by RequireClientCredentials middleware)
	serviceAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "middleware did not supply service account ID")
		return
	}

	// Get service account details to return client_id
	serviceAccount, err := a.queries.GetServiceAccountByAccountID(r.Context(), serviceAccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to get service account details")
		return
	}

	// Generate new client secret
	newPlaintextSecret, err := a.authService.GenerateSecureToken(32)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "failed to generate secure token")
		return
	}
	hashedSecret := a.authService.HashToken(newPlaintextSecret)
	expiresAt := time.Now().Add(signalsd.ClientSecretExpiry)

	tx, err := a.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to begin transaction")
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

		}
	}()

	txQueries := a.queries.WithTx(tx)

	_, err = txQueries.ScheduleRevokeAllClientSecretsForAccount(r.Context(), serviceAccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to schedule revocation of old secrets")
		return
	}

	_, err = txQueries.CreateClientSecret(r.Context(), database.CreateClientSecretParams{
		HashedSecret:            hashedSecret,
		ServiceAccountAccountID: serviceAccountID,
		ExpiresAt:               expiresAt,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to create new secret")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "failed to commit transaction")
		return
	}

	logger.ContextWithLogAttrs(r.Context(),
		slog.String("client_id", serviceAccount.ClientID),
	)

	response := ServiceAccountRotateResponse{
		ClientID:     serviceAccount.ClientID,
		ClientSecret: newPlaintextSecret,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		ExpiresIn:    int(signalsd.ClientSecretExpiry.Seconds()),
	}

	responses.RespondWithJSON(w, http.StatusOK, response)
}
