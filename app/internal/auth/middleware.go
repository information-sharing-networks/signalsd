package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
)

type ServiceAccountTokenRequest struct {
	ClientID     string `json:"client_id" example:"sa_example-org_k7j2m9x1"`
	ClientSecret string `json:"client_secret" example:"dGhpcyBpcyBhIHNlY3JldA"`
}

// RequireValidAccessToken checks that the supplied access token is well formed, correctly signed and has not expired.
//
// If allowExpired is true, the token is still checked to confirm it parses and has a valid signature but expired tokens are accepted
// (this option is used by /oauth/revoke, which needs to use the userAccountID in the expired token to determine which refresh token to revoke when handling web users)
//
// with the execption of expiry errors details of authentication failures are not supplied in the response (cliets just get 'unauthorised')
//
// if the access token is valid the requestor's accountID, accountType and jwt claims are added to the Context.
func (a *AuthService) RequireValidAccessToken(allowExpired bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			logger := zerolog.Ctx(r.Context())

			accessToken, err := a.GetAccessTokenFromHeader(r.Header)
			if err != nil {
				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
				return
			}

			claims := &Claims{}

			// extract the claims from the jwt and validate the signature
			_, err = jwt.ParseWithClaims(accessToken, claims, func(token *jwt.Token) (any, error) {
				return []byte(a.secretKey), nil
			})
			if err != nil {
				if errors.Is(err, jwt.ErrTokenExpired) {
					if !allowExpired {
						responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAccessTokenExpired, "access token expired, please use the /oauth/token endpoint to renew it")
						return
					}
				} else {
					responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
					return
				}
			}

			accountIDString := claims.Subject

			accountID, err := uuid.Parse(accountIDString)
			if err != nil {
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("signed jwt received without a valid accountID in sub: %v", err))
				return
			}

			if allowExpired {
				logger.Info().Msgf("account %v access_token validated (allowExpired = true)", accountID)
			} else {
				logger.Info().Msgf("account %v access_token validated", accountID)
			}

			// add user and claims to context
			ctx := ContextWithAccountID(r.Context(), accountID)
			ctx = ContextWithAccountType(ctx, claims.AccountType)
			ctx = ContextWithClaims(ctx, claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthenticateByGrantType calls the appropriate authentication middleware based on the grant_type URL param
//
// - grant_type = client_credentials (service accounts): a valid Client ID/Client Secret is required
//
// - grant_type = refesh_token (web user accounts): a valid Access Token and valid Refresh token are required.  It does not matter if the access token has expired.
//
// This middleware should be called before allowing an account to get a new access token.
func (a *AuthService) AuthenticateByGrantType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grantType := r.URL.Query().Get("grant_type")

		switch grantType {
		case "client_credentials":
			a.RequireValidClientCredentials(next).ServeHTTP(w, r)
		case "refresh_token":
			a.RequireValidAccessToken(true)(a.RequireValidRefreshToken(next)).ServeHTTP(w, r)
		default:
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, fmt.Sprintf("invalid grant_type parameter in URL %v", grantType))
			return
		}
	})
}

// AuthenticateByCredentalType determines the authentication method based on request structure:
//
// - If Authorization header is present: Web user (requires bearer token + refresh token cookie)
//
// - If no Authorization header: Service account (requires client credentials in JSON body)
//
// This middleware should be called before allowing an account to revoke a refresh token or client secret
func (a *AuthService) AuthenticateByCredentalType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if authHeader != "" {
			a.RequireValidAccessToken(true)(a.RequireValidRefreshToken(next)).ServeHTTP(w, r)
		} else {
			// Service account request: No Authorization header
			a.RequireValidClientCredentials(next).ServeHTTP(w, r)
		}
	})
}

// RequireValidRefreshToken checks that the refresh token - which is used by web users to get new access tokens - is not expired or revoked
//
// If the token is valid the hashedRefreshToken to the Context.
func (a *AuthService) RequireValidRefreshToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		logger := zerolog.Ctx(r.Context())

		claims, ok := ContextClaims(r.Context())
		if !ok {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
			return
		}

		if claims.AccountType != "user" {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, "refresh tokens are only valid for user accounts")
			return
		}

		userAccountID, ok := ContextAccountID(r.Context())
		if !ok {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get userAccountID from context")
			return
		}

		// check the database for an unexpired/unrevoked refresh token for the user and - if there isn't one  - tell the user to login in again
		returnedRefreshTokenRow, err := a.queries.GetValidRefreshTokenByUserAccountId(r.Context(), userAccountID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: session expired, please log in again")
				return
			}
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
			return
		}

		// extract the refresh token from the cookie
		cookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
		if err != nil {
			if err == http.ErrNoCookie {
				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: refresh_token cookie not found")
				return
			}
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not read cookie: %v", err))
			return
		}

		// authenticate the request
		ok = a.CheckTokenHash(returnedRefreshTokenRow.HashedToken, cookie.Value)
		if !ok {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: invalid token, please log in again")
			return
		}

		logger.Info().Msgf("user %v refresh_token validated", userAccountID)

		// add userId and hashedRefreshToken to context

		ctx := ContextWithAccountID(r.Context(), userAccountID)
		ctx = ContextWithAccountType(ctx, "user")
		ctx = ContextWithHashedRefreshToken(ctx, returnedRefreshTokenRow.HashedToken) // needed by RevokeRefreshTokenHandler

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireValidClientCredentials checks the client crentials used to authenticate service accounts
//
// no bearer token required - authenticaton is done using the client credentials.
func (a *AuthService) RequireValidClientCredentials(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var req ServiceAccountTokenRequest
		logger := zerolog.Ctx(r.Context())

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
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "invalid client_id")
			return
		}

		// check if the service account is active
		account, err := a.queries.GetAccountByID(r.Context(), serviceAccount.AccountID)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "account not found")
			return
		}

		if !account.IsActive {
			logger.Warn().Msgf("attempt to authenticate with disabled service account: %v", serviceAccount.AccountID)
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "account is disabled")
			return
		}

		// check the client secret
		hashedSecret := a.HashToken(req.ClientSecret)

		_, err = a.queries.GetValidClientSecretByHashedSecret(r.Context(), hashedSecret)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "invalid client secret")
			return
		}

		logger.Info().Msgf("Client credentials confirmed for service account ID %v", serviceAccount.AccountID)
		ctx := ContextWithAccountID(r.Context(), serviceAccount.AccountID)
		ctx = ContextWithAccountType(ctx, "service_account")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// check the account role in the jwt claims matches one of the roles supplied in the function call.
// RequireRole hould only be used after RequireValidAccessToken middlware, which adds the claims (including the account role) to the context
func (a *AuthService) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := zerolog.Ctx(r.Context())
			claims, ok := ContextClaims(r.Context())
			if !ok {
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
				return
			}

			for _, role := range allowedRoles {
				if claims.Role == role {
					logger.Info().Msgf("Role confirmed: %v", role)
					next.ServeHTTP(w, r)
					return
				}
			}
			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you do not have permission to use this feature")
		})
	}
}

// RequireIsnPermission checks the access_token claims to ensure the account has one of the supplied permissions (read/write) for the ISN specified in the isn_slug URL parameter
//
// This middleware should only be used after RequireValidAccessToken middlware, which adds the claims in the context
func (a *AuthService) RequireIsnPermission(allowedPermissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			logger := zerolog.Ctx(r.Context())

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

			for _, permission := range allowedPermissions {
				if claims.IsnPerms[isnSlug].Permission == permission {
					logger.Info().Msgf("Permission confirmed: %v", permission)
					next.ServeHTTP(w, r)
					return
				}
			}

			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you do not have the necessary access permission for this isn")
		})
	}
}

func (a *AuthService) RequireDevEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		logger := zerolog.Ctx(r.Context())

		if a.environment != "dev" {
			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this api can only be used in the dev environment")
			return
		}
		logger.Info().Msg("Dev environment confirmed")
		next.ServeHTTP(w, r)
	})
}
