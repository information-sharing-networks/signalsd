package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	signalsd "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/response"
	"github.com/rs/zerolog"
)

// checks that access tokens are valid (signed, correctly set of claims, not expired)
//
// Adds userAccountID and claims to context.
func (a AuthService) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		reqLogger := zerolog.Ctx(r.Context())

		accessToken, err := a.GetAccessTokenFromHeader(r.Header)
		if err != nil {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		claims := &AccessTokenClaims{}

		_, err = jwt.ParseWithClaims(accessToken, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(a.secretKey), nil
		})
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAccessTokenExpired, "access token expired, please use the refresh api to renew it")
				return
			}
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		userAccountIDString := claims.Subject
		userAccountID, err := uuid.Parse(userAccountIDString)
		if err != nil {
			response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("signed jwt received without a valid userAccountID in sub: %v", err))
			return
		}

		reqLogger.Info().Msgf("user %v access_token sucessfully authenticated", userAccountID)

		// add user and claims to context
		ctx := ContextWithUserAccountID(r.Context(), userAccountID)
		ctx = ContextWithAccessTokenClaims(ctx, claims)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// check that the refresh token is not expired or revoked
// Note that a bearer access token is required in order to identify the user
// adds userAccountID and hashedRefreshToken to context
func (a AuthService) RequireValidRefreshToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		reqLogger := zerolog.Ctx(r.Context())

		// get access token
		accessToken, err := a.GetAccessTokenFromHeader(r.Header)
		if err != nil {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		// validate signature and get the user.

		/* Expired tokens are expected here:
		- the main reason to validate a refresh token is that a client wants to refresh an expired access token
		- (this is safe because the token is not used for authentication, only as a means to identify the user) */

		claims := jwt.RegisteredClaims{}

		_, err = jwt.ParseWithClaims(accessToken, &claims, func(token *jwt.Token) (any, error) {
			return []byte(a.secretKey), nil
		})
		if err != nil && !errors.Is(err, jwt.ErrTokenExpired) {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		userAccountID, err := uuid.Parse(claims.Subject)
		if err != nil {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("could not get user from claims: %v", err))
			return
		}

		// check the database for an unexpired/unrevoked refresh token for the user and - if there isn't one  - tell the user to login in again
		returnedRefreshTokenRow, err := a.queries.GetValidRefreshTokenByUserAccountId(r.Context(), userAccountID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: session expired, please log in again")
				return
			}
			response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
			return
		}

		// extract the refresh token from the cookie
		cookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
		if err != nil {
			if err == http.ErrNoCookie {
				response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: refresh_token cookie not found")
				return
			}
			response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not read cookie: %v", err))
			return
		}

		// authenticate the request
		ok := a.CheckTokenHash(returnedRefreshTokenRow.HashedToken, cookie.Value)
		if !ok {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: invalid token, please log in again")
			return
		}

		reqLogger.Info().Msgf("user %v refresh_token validated", userAccountID)

		// userAccountID and hashedRefreshToken are needed by the token handler
		ctx := ContextWithUserAccountID(r.Context(), userAccountID)
		ctx = ContextWithHashedRefreshToken(ctx, returnedRefreshTokenRow.HashedToken)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a AuthService) RequireDevEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		reqLogger := zerolog.Ctx(r.Context())

		if a.environment != "dev" {
			response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this api can only be used in the dev environment")
			return
		}
		reqLogger.Info().Msg("Dev environment confirmed")
		next.ServeHTTP(w, r)
	})
}
func (a AuthService) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ContextAccessTokenClaims(r.Context())
			if !ok {
				response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
				return
			}

			for _, role := range allowedRoles {
				if claims.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}

			response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you do not have permission to use this feature")
		})
	}
}
