package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/context"
	"github.com/nickabs/signalsd/app/internal/response"
	"github.com/rs/zerolog/log"
)

func (a AuthService) ValidateAccessToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		reqLogger := log.With().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Logger()

		bearerToken, err := a.BearerTokenFromHeader(r.Header)
		if err != nil {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		claims := jwt.RegisteredClaims{}

		_, err = jwt.ParseWithClaims(bearerToken, &claims, func(token *jwt.Token) (interface{}, error) {
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

		rawID := claims.Subject
		userAccountID, err := uuid.Parse(rawID)
		if err != nil {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		reqLogger.Info().Msgf("user %v authorized ", userAccountID)

		// add user to context
		ctx := context.WithUserAccountID(r.Context(), userAccountID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a AuthService) ValidateRefreshToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		reqLogger := log.With().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Logger()

		refreshToken, err := a.BearerTokenFromHeader(r.Header)
		if err != nil {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeTokenError, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		refreshTokenRow, err := a.queries.GetRefreshToken(r.Context(), refreshToken)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeTokenError, "unauthorized: Invalid token")
				return
			}
			response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenError, fmt.Sprintf("database error: %v", err))
			return
		}
		if refreshTokenRow.ExpiresAt.In(time.UTC).Before(time.Now().In(time.UTC)) {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenExpired, "the supplied token has expired - please login again ")
			return
		}
		if refreshTokenRow.RevokedAt.Valid {
			response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeRefreshTokenRevoked, "the supplied token was revoked previously - please login again")
			return
		}

		reqLogger.Info().Msgf("user %v refresh_token validated", refreshTokenRow.UserAccountID)

		ctx := context.WithUserAccountID(r.Context(), refreshTokenRow.UserAccountID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a AuthService) ValidateDevEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		reqLogger := log.With().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Logger()

		if a.environment != "dev" {
			response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this api can only be used in the dev environment")
			return
		}
		reqLogger.Info().Msg("Dev environment confirmed")
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}
