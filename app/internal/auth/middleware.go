package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/jackc/pgx/v5"
)

type ServiceAccountTokenRequest struct {
	ClientID     string `json:"client_id" example:"sa_example-org_k7j2m9x1"`
	ClientSecret string `json:"client_secret" example:"dGhpcyBpcyBhIHNlY3JldA"`
}

// RequireValidAccessToken checks that the supplied access token is well formed, correctly signed and has not expired.
//
// Details of authentication failures are not supplied in the response (clients just get 'unauthorised'),
// with the exception of expiry errors which get a specific error code.
//
// If the access token is valid the requestor's accountID, accountType and jwt claims are added to the Context.
//
// Note this middleware adds the account id, role and account type as log attributes to the context and
// these fields will automatically be included in the final request log for all requests that require an access token.
func (a *AuthService) RequireValidAccessToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		accessToken, err := a.GetAccessTokenFromHeader(r.Header)
		if err != nil {
			// Add context for final request log
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, "unauthorized")
			return
		}

		claims := &Claims{}

		// extract the claims from the jwt and validate the signature
		// WithValidMethods ensures only HS256 tokens are accepted, preventing algorithm confusion attacks
		_, err = jwt.ParseWithClaims(accessToken, claims, func(token *jwt.Token) (any, error) {
			return []byte(a.secretKey), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("account_id", claims.AccountID.String()),
					slog.String("error", err.Error()),
				)

				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAccessTokenExpired, "access token expired, please use the /oauth/token endpoint to renew it")
				return
			} else {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("account_id", claims.AccountID.String()),
					slog.String("error", err.Error()),
				)

				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, "unauthorized")
				return
			}
		}

		accountIDString := claims.Subject

		accountID, err := uuid.Parse(accountIDString)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("account_id", claims.AccountID.String()),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "unauthorized - error processing access token")
			return
		}

		reqLogger.Debug("Access token validation successful",
			slog.String("component", "signalsd.RequireValidAccessToken"),
			slog.String("account_id", accountID.String()),
			slog.String("account_type", claims.AccountType),
		)

		// Add account_id, role and account_type to final request log context
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("account_id", accountID.String()),
			slog.String("account_type", claims.AccountType),
			slog.String("role", claims.Role),
		)

		// add user and claims to context
		ctx := ContextWithAccountID(r.Context(), accountID)
		ctx = ContextWithAccountType(ctx, claims.AccountType)
		ctx = ContextWithClaims(ctx, claims)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuthByGrantType - checks authentiation before issuing a new access token.
//
// The funciton calls the appropriate authentication middleware based on the grant_type URL param:
//
//   - grant_type = client_credentials: service accounts - RequireValidClientCredentials
//   - grant_type = refresh_token: web users - RequireValidRefreshToken
//
// This middleware should be called before allowing an account to get a new access token (/oauth/token)
func (a *AuthService) RequireAuthByGrantType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grantType := r.URL.Query().Get("grant_type")

		switch grantType {
		case "client_credentials":
			a.RequireValidClientCredentials(next).ServeHTTP(w, r)
		case "refresh_token":
			a.RequireValidRefreshToken(next).ServeHTTP(w, r)
		default:
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid grant_type parameter")
			return
		}
	})
}

// RequireAuthForCredentialType - checks auth before revoking a refresh token.
//
// the authentication method is determined based on request structure:
//   - If refresh token cookie is present: Web user - RequireValidRefreshToken
//   - If no refresh token cookie: Service account - RequireValidClientCredentials
//
// This middleware should be called before allowing an account to revoke a refresh token or client secret.
func (a *AuthService) RequireAuthForCredentialType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for refresh token cookie to determine if this is a web user request
		_, err := r.Cookie(signalsd.RefreshTokenCookieName)

		if err == nil {
			// Web user request: Has refresh token cookie
			a.RequireValidRefreshToken(next).ServeHTTP(w, r)
		} else {
			// Service account request: No refresh token cookie
			a.RequireValidClientCredentials(next).ServeHTTP(w, r)
		}
	})
}

// RequireValidRefreshToken checks that the refresh token is present, not expired and not revoked.
//
// The refresh token is retieved from the cookie named [ignalsd.RefreshTokenCookieName].
//
// If the token is valid, the userAccountID, accountType, and hashedRefreshToken are added to the Context.
func (a *AuthService) RequireValidRefreshToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// extract the refresh token from the cookie
		cookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
		if err != nil {
			if err == http.ErrNoCookie {
				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: refresh_token cookie not found")
				return
			}
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
			return
		}

		// hash the refresh token to look it up in the database
		hashedToken := a.HashToken(cookie.Value)

		// check the database for a valid (unexpired/unrevoked) refresh token
		refreshTokenRow, err := a.queries.GetValidRefreshTokenByHashedToken(r.Context(), hashedToken)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: session expired, please log in again")
				return
			}
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

		userAccountID := refreshTokenRow.UserAccountID

		reqLogger := logger.ContextRequestLogger(r.Context())

		// Log successful refresh token validation immediately
		reqLogger.Debug("Refresh token validation successful",
			slog.String("component", "signalsd.RequireValidRefreshToken"),
			slog.String("account_id", userAccountID.String()),
		)

		// Add user_account_id to final request log context
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("account_id", userAccountID.String()),
		)

		// add userId, accountType and hashedRefreshToken to context
		ctx := ContextWithAccountID(r.Context(), userAccountID)
		ctx = ContextWithAccountType(ctx, "user")
		ctx = ContextWithHashedRefreshToken(ctx, hashedToken) // needed by RevokeRefreshTokenHandler

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireValidClientCredentials checks the client crentials used to authenticate service accounts.
//
// The Client ID/Client Secret is extraced from the request body.
// No bearer token required - authenticaton is done using the client credentials alone.
//
// This middleware adds the client_id to the log attributes in context so they
// are included in the request log for any requests authenticated with client credentials
func (a *AuthService) RequireValidClientCredentials(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var req ServiceAccountTokenRequest

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
			return

		}

		if req.ClientID == "" || req.ClientSecret == "" {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "client_id and client_secret are required")
			return
		}

		serviceAccount, err := a.queries.GetServiceAccountByClientID(r.Context(), req.ClientID)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("invalid_client_id", req.ClientID),
			)

			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "invalid client_id")
			return
		}

		// get account
		account, err := a.queries.GetAccountByID(r.Context(), serviceAccount.AccountID)
		if err != nil {
			if err == sql.ErrNoRows {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("client_id", req.ClientID),
					slog.String("error", err.Error()),
				)

				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "account not found")
				return
			}
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("client_id", req.ClientID),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "database error")
			return
		}

		if !account.IsActive {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("client_id", req.ClientID),
				slog.String("account_id", serviceAccount.AccountID.String()),
			)

			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "account is disabled")
			return
		}

		// check the client secret
		hashedSecret := a.HashToken(req.ClientSecret)

		_, err = a.queries.GetValidClientSecretByHashedSecret(r.Context(), hashedSecret)
		if err != nil {
			// Add context for final request log
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("client_id", req.ClientID),
			)

			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "invalid client secret")
			return
		}

		reqLogger := logger.ContextRequestLogger(r.Context())

		// Log successful client credentials validation
		reqLogger.Debug("Client credentials validation successful",
			slog.String("component", "signalsd.RequireValidClientCredentials"),
			slog.String("client_id", req.ClientID),
			slog.String("account_id", serviceAccount.AccountID.String()),
		)

		// Add context for final request log
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("client_id", req.ClientID),
			slog.String("account_id", serviceAccount.AccountID.String()),
		)

		ctx := ContextWithAccountID(r.Context(), serviceAccount.AccountID)
		ctx = ContextWithAccountType(ctx, "service_account")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole checks the account role in the jwt claims matches one of the supplied roles.
//
// RequireRole should only be used after RequireValidAccessToken middlware,
// which adds the claims to the context.
func (a *AuthService) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			reqLogger := logger.ContextRequestLogger(r.Context())

			claims, ok := ContextClaims(r.Context())
			if !ok {
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
				return
			}
			for _, role := range allowedRoles {
				if claims.Role == role {
					reqLogger.Debug("Role authorization successful",
						slog.String("component", "signalsd.RequireAuth"),
						slog.String("account_id", claims.AccountID.String()),
						slog.Any("allowed_roles", allowedRoles),
						slog.String("role", role),
					)

					next.ServeHTTP(w, r)
					return
				}
			}

			// let the logger middleware handle log message
			logger.ContextWithLogAttrs(r.Context(),
				slog.Any("expected_roles", allowedRoles),
				slog.String("got_role", claims.Role),
			)
			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, fmt.Sprintf("account does not have required role - must be one of %v", strings.Join(allowedRoles, ", ")))
		})
	}
}

// RequireAccessPermission checks the account has permission to access the data in the ISN identified in the URL path.
//
// To use the ISN the account must have the specified permissions (read and/or write) supplied in the function call for the
// ISN specified in the isn_slug URL parameter. The middleware also checks the ISN is active.
//
// Where the middleware is used to protect signal type specific endpoints,
// it also validates that the signal type is in use on the specified ISN.
//
// This middleware uses the claims to determine permissions and should therefore only be used after
// RequireValidAccessToken middlware, which adds the claims in the context.
func (a *AuthService) RequireAccessPermission(permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			claims, ok := ContextClaims(r.Context())
			if !ok {
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
				return
			}

			isnSlug := r.PathValue("isn_slug")
			if isnSlug == "" {
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInvalidRequest, "no isn_slug parameter ")
				return
			}

			signalTypeSlug := r.PathValue("signal_type_slug")
			semVer := r.PathValue("sem_ver")
			signalTypePath := ""
			if signalTypeSlug != "" && semVer != "" {
				signalTypePath = fmt.Sprintf("%s/v%s", signalTypeSlug, semVer)
			}

			// check the account has at least one of the specified read/write permission(s)
			var lastErr error
			for _, permission := range permissions {
				switch permission {
				case "read":
					lastErr = CheckIsnReadPermission(claims, isnSlug, signalTypePath)
				case "write":
					lastErr = CheckIsnWritePermission(claims, isnSlug, signalTypePath)
				default:
					lastErr = &PermissionError{
						Status:  http.StatusInternalServerError,
						Code:    apperrors.ErrCodeInternalError,
						Message: fmt.Sprintf("unknown permission %q", permission),
					}
				}
				if lastErr == nil {
					break
				}
			}

			if lastErr != nil {
				if permErr, ok := lastErr.(*PermissionError); ok {
					responses.RespondWithError(w, r, permErr.Status, permErr.Code, permErr.Message)
				} else {
					responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "permission check failed")
				}
				return
			}

			logger.ContextWithLogAttrs(r.Context(),
				slog.String("isn_permission", strings.Join(permissions, ",")),
				slog.String("isn_slug", isnSlug),
			)
			next.ServeHTTP(w, r)
		})
	}
}

// RequireIsnMembership checks that the account has been granted access to the ISN and that the ISN is in use.
//
// Where signal_type_slug and sem_ver are present in the URL it also checks the signal type is in use.
//
// It does not matter what permissions the account has (can be read, write or both).
// Use this instead of RequireAccessPermission for endpoints where write-only accounts are valid callers.
//
// This middleware should only be used after RequireValidAccessToken middleware, which adds the claims in the context.
func (a *AuthService) RequireIsnMembership(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		claims, ok := ContextClaims(r.Context())
		if !ok {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
			return
		}

		isnSlug := r.PathValue("isn_slug")
		if isnSlug == "" {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInvalidRequest, "no isn_slug parameter")
			return
		}

		perms, ok := claims.IsnPerms[isnSlug]
		if !ok {
			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "account does not have access to this isn")
			return
		}

		if !perms.CanRead && !perms.CanWrite {
			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "account does not have access to this isn")
			return
		}

		if !perms.InUse {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not in use")
			return
		}

		signalTypeSlug := r.PathValue("signal_type_slug")
		semVer := r.PathValue("sem_ver")

		if signalTypeSlug != "" && semVer != "" {
			signalTypePath := fmt.Sprintf("%s/v%s", signalTypeSlug, semVer)
			signalType, found := perms.SignalTypes[signalTypePath]
			if !found {
				responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "signal type not found on this ISN")
				return
			}
			if !signalType.InUse {
				responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "signal type not in use")
				return
			}
		}

		logger.ContextWithLogAttrs(r.Context(),
			slog.String("isn_slug", isnSlug),
		)
		next.ServeHTTP(w, r)
	})
}

// RequireDevEnv - checks the server was started as a dev environment instance
func (a *AuthService) RequireDevEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if a.environment != "dev" {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("environment", a.environment),
			)

			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this api can only be used in the dev environment")
			return
		}

		next.ServeHTTP(w, r)
	})
}
