package auth

import (
	"database/sql"
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
			responses.RenderError(w, r, &apperrors.HTTPError{
				Status:  http.StatusUnauthorized,
				Code:    apperrors.ErrCodeAuthorizationFailure,
				Message: "unauthorized",
				Err:     err,
			})
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
				)
				responses.RenderError(w, r, &apperrors.HTTPError{
					Status:  http.StatusUnauthorized,
					Code:    apperrors.ErrCodeAccessTokenExpired,
					Message: "access token expired, please use the /oauth/token endpoint to renew it",
					Err:     err,
				})
				return
			}
			responses.RenderError(w, r, &apperrors.HTTPError{
				Status:  http.StatusUnauthorized,
				Code:    apperrors.ErrCodeAuthorizationFailure,
				Message: "unauthorized",
				Err:     err,
			})
			return
		}

		accountIDString := claims.Subject

		accountID, err := uuid.Parse(accountIDString)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("account_id", claims.AccountID.String()),
			)

			responses.RenderError(w, r, apperrors.InternalError("unauthorized - error processing access token", err))
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

// RequireAuthByGrantType - checks authentication before issuing a new access token.
//
// The function calls the appropriate authentication middleware based on the grant_type form field (RFC 6749):
//
//   - grant_type = client_credentials: service accounts - RequireValidClientCredentials
//   - grant_type = refresh_token: web users - RequireValidRefreshToken
//
// This middleware should be called before allowing an account to get a new access token (/oauth/token) or revoke a token (/oauth/revoke)
func (a *AuthService) RequireAuthByGrantType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grantType := r.PostFormValue("grant_type")

		switch grantType {
		case "client_credentials":
			a.RequireValidClientCredentials(next).ServeHTTP(w, r)
		case "refresh_token":
			a.RequireValidRefreshToken(next).ServeHTTP(w, r)
		default:
			responses.RenderError(w, r, apperrors.OAuthUnsupportedGrantType("grant_type must be client_credentials or refresh_token", apperrors.ErrCodeInvalidRequest, nil))
			return
		}
	})
}

// RequireValidRefreshToken checks that the refresh token is present, not expired and not revoked.
//
// # The refresh token is read from the HTTP-only cookie
//
// If the token is valid, the userAccountID, accountType, and hashedRefreshToken are added to the Context.
func (a *AuthService) RequireValidRefreshToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// extract the refresh token
		cookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
		if err != nil {
			if err == http.ErrNoCookie {
				responses.RenderError(w, r, apperrors.OAuthInvalidGrant("refresh_token cookie not found", apperrors.ErrCodeRefreshTokenInvalid, nil))
				return
			}
			responses.RenderError(w, r, apperrors.InternalError("internal server error", err))
			return
		}

		// hash the refresh token to look it up in the database
		hashedToken := a.HashToken(cookie.Value)

		// check the database for a valid (unexpired/unrevoked) refresh token
		refreshTokenRow, err := a.queries.GetValidRefreshTokenByHashedToken(r.Context(), hashedToken)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				responses.RenderError(w, r, apperrors.OAuthInvalidGrant("session expired, please log in again", apperrors.ErrCodeRefreshTokenInvalid, nil))
				return
			}
			responses.RenderError(w, r, apperrors.DatabaseError("database error", err))
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

		var clientID, clientSecret string

		// support HTTP Basic Auth or form body (RFC 6749)
		if id, secret, ok := r.BasicAuth(); ok {
			clientID = id
			clientSecret = secret
		} else {
			clientID = r.PostFormValue("client_id")
			clientSecret = r.PostFormValue("client_secret")
		}

		if clientID == "" || clientSecret == "" {
			responses.RenderError(w, r, apperrors.OAuthInvalidRequest("client_id and client_secret are required", apperrors.ErrCodeInvalidRequest, nil))
			return
		}

		serviceAccount, err := a.queries.GetServiceAccountByClientID(r.Context(), clientID)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("invalid_client_id", clientID),
			)

			responses.RenderError(w, r, apperrors.OAuthInvalidClient("invalid client_id", apperrors.ErrCodeAuthenticationFailure, nil))
			return
		}

		// get account
		account, err := a.queries.GetAccountByID(r.Context(), serviceAccount.AccountID)
		if err != nil {
			if err == sql.ErrNoRows {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("client_id", clientID),
				)

				responses.RenderError(w, r, apperrors.InternalError("account not found", err))
				return
			}
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("client_id", clientID),
			)

			responses.RenderError(w, r, apperrors.InternalError("database error", err))
			return
		}

		if !account.IsActive {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("client_id", clientID),
				slog.String("account_id", serviceAccount.AccountID.String()),
			)

			responses.RenderError(w, r, apperrors.OAuthInvalidClient("account is disabled", apperrors.ErrCodeAuthorizationFailure, nil))
			return
		}

		// check the client secret
		hashedSecret := a.HashToken(clientSecret)

		_, err = a.queries.GetValidClientSecretByHashedSecret(r.Context(), hashedSecret)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("client_id", clientID),
			)

			responses.RenderError(w, r, apperrors.OAuthInvalidClient("invalid client secret", apperrors.ErrCodeAuthenticationFailure, nil))
			return
		}

		reqLogger := logger.ContextRequestLogger(r.Context())

		// Log successful client credentials validation
		reqLogger.Debug("Client credentials validation successful",
			slog.String("component", "signalsd.RequireValidClientCredentials"),
			slog.String("client_id", clientID),
			slog.String("account_id", serviceAccount.AccountID.String()),
		)

		// Add context for final request log
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("client_id", clientID),
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
				responses.RenderError(w, r, apperrors.InternalError("could not get claims from context", nil))
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
			responses.RenderError(w, r, apperrors.Forbidden(fmt.Sprintf("account does not have required role - must be one of %v", strings.Join(allowedRoles, ", ")), nil))
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
				responses.RenderError(w, r, apperrors.InternalError("could not get claims from context", nil))
				return
			}

			isnSlug := r.PathValue("isn_slug")
			if isnSlug == "" {
				responses.RenderError(w, r, apperrors.InternalError("no isn_slug parameter", nil))
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
					responses.RenderError(w, r, &apperrors.HTTPError{
						Status:  permErr.Status,
						Code:    permErr.Code,
						Message: permErr.Message,
					})
				} else {
					responses.RenderError(w, r, apperrors.InternalError("permission check failed", nil))
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
			responses.RenderError(w, r, apperrors.InternalError("could not get claims from context", nil))
			return
		}

		isnSlug := r.PathValue("isn_slug")
		if isnSlug == "" {
			responses.RenderError(w, r, apperrors.InternalError("no isn_slug parameter", nil))
			return
		}

		perms, ok := claims.IsnPerms[isnSlug]
		if !ok {
			responses.RenderError(w, r, apperrors.Forbidden("account does not have access to this isn", nil))
			return
		}

		if !perms.CanRead && !perms.CanWrite {
			responses.RenderError(w, r, apperrors.Forbidden("account does not have access to this isn", nil))
			return
		}

		if !perms.InUse {
			responses.RenderError(w, r, apperrors.NotFound("ISN not in use", nil))
			return
		}

		signalTypeSlug := r.PathValue("signal_type_slug")
		semVer := r.PathValue("sem_ver")

		if signalTypeSlug != "" && semVer != "" {
			signalTypePath := fmt.Sprintf("%s/v%s", signalTypeSlug, semVer)
			signalType, found := perms.SignalTypes[signalTypePath]
			if !found {
				responses.RenderError(w, r, apperrors.NotFound("signal type not found on this ISN", nil))
				return
			}
			if !signalType.InUse {
				responses.RenderError(w, r, apperrors.NotFound("signal type not in use", nil))
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

			responses.RenderError(w, r, apperrors.Forbidden("this api can only be used in the dev environment", nil))
			return
		}

		next.ServeHTTP(w, r)
	})
}
