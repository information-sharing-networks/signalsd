package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	signalsd "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/server/responses"
	"github.com/rs/zerolog"
)

// checks that access tokens are valid (signed, correctly set of claims, not expired)
//
// Adds accountID and claims to context.
func (a AuthService) RequireValidAccessToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		logger := zerolog.Ctx(r.Context())

		accessToken, err := a.GetAccessTokenFromHeader(r.Header)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		claims := &AccessTokenClaims{}

		_, err = jwt.ParseWithClaims(accessToken, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(a.secretKey), nil
		})
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAccessTokenExpired, "access token expired, please use the refresh api to renew it")
				return
			}
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		accountIDString := claims.Subject
		accountID, err := uuid.Parse(accountIDString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("signed jwt received without a valid accountID in sub: %v", err))
			return
		}

		logger.Info().Msgf("user %v access_token sucessfully authenticated", accountID)

		// add user and claims to context
		ctx := ContextWithAccountID(r.Context(), accountID)
		ctx = ContextWithAccessTokenClaims(ctx, claims)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// check the claimed role matches the allowedRoles
// should only be used after RequireValidAccessToken middlware, which supplies the claims
func (a AuthService) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := zerolog.Ctx(r.Context())
			claims, ok := ContextAccessTokenClaims(r.Context())
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

// check the access_token claims to ensure they can write to this isn
// should only be used after RequireValidAccessToken middlware, which supplies the claims
func (a AuthService) RequireIsnWritePermission() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			logger := zerolog.Ctx(r.Context())

			claims := &AccessTokenClaims{}

			claims, ok := ContextAccessTokenClaims(r.Context())
			if !ok {
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "check isn write permission: could not get claims from context")
				return
			}

			isnSlug := chi.URLParam(r, "isn_slug")
			if isnSlug == "" {
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInvalidRequest, "check isn write permission: no isn_slug parameter ")
				return
			}

			if claims.IsnPerms[isnSlug].Permission == "write" {
				logger.Debug().Msgf("user %v write access confirmed for isn %v", claims.Subject, isnSlug)
				next.ServeHTTP(w, r)
				return
			}

			responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you do not have permission to write to this isn")
		})
	}
}

// check that the refresh token is not expired or revoked
// Note that a bearer access token is required in order to identify the user
// adds userAccountID and hashedRefreshToken to context
func (a AuthService) RequireValidRefreshToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		logger := zerolog.Ctx(r.Context())

		// get access token
		accessToken, err := a.GetAccessTokenFromHeader(r.Header)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
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
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		userAccountID, err := uuid.Parse(claims.Subject)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("could not get user from claims: %v", err))
			return
		}

		// check the database for an unexpired/unrevoked refresh token for the user and - if there isn't one  - tell the user to login in again
		returnedRefreshTokenRow, err := a.queries.GetValidRefreshTokenByUserAccountId(r.Context(), userAccountID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
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
		ok := a.CheckTokenHash(returnedRefreshTokenRow.HashedToken, cookie.Value)
		if !ok {
			responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenInvalid, "unauthorised: invalid token, please log in again")
			return
		}

		logger.Info().Msgf("user %v refresh_token validated", userAccountID)

		// userAccountID and hashedRefreshToken are needed by the token handler
		ctx := ContextWithAccountID(r.Context(), userAccountID)
		ctx = ContextWithHashedRefreshToken(ctx, returnedRefreshTokenRow.HashedToken)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a AuthService) RequireDevEnv(next http.Handler) http.Handler {
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
