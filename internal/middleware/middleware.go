package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/auth"
	"github.com/nickabs/signals/internal/helpers"
	"github.com/rs/zerolog/log"
)

func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		reqLogger := log.With().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Logger()

		ctx := context.WithValue(r.Context(), signals.RequestLoggerKey, &reqLogger)

		reqLogger.Info().Msg("Request started")

		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r.WithContext(ctx))

		reqLogger.Info().
			Int("status", ww.Status()).
			Dur("duration_ms", time.Since(start)).
			Int("bytes_written", ww.BytesWritten()).
			Msg("Request completed")
	})
}

// this middleware sets a contextKey with the userID where authorized endpoints are handled
func AuthorizationMiddleware(authService auth.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := middleware.GetReqID(r.Context())
			reqLogger := log.With().
				Str("request_id", requestID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Logger()

			ctx := r.Context()

			// default to authentication check
			switch {
			case r.Method == http.MethodGet:
				reqLogger.Info().Msg("GET request, skipping authorization check")

			case r.Method == http.MethodPost && r.URL.Path == "/api/login":
				reqLogger.Info().Msg("login, skipping authorization check")

			case r.Method == http.MethodPost && r.URL.Path == "/api/refresh":
				reqLogger.Info().Msg("refresh access code, skipping authorization check")

			case r.Method == http.MethodPost && r.URL.Path == "/api/users":
				reqLogger.Info().Msg("creating user, skipping authorization check")

			// reset is unprotected but will only work on dev
			case r.Method == http.MethodPost && r.URL.Path == "/admin/reset":
				reqLogger.Info().Msg("reset database, skipping authorization check")

			default:
				userID, err := authService.CheckAuthorization(r.Header)
				if err != nil {
					helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
					return
				}
				ctx = context.WithValue(ctx, signals.UserIDKey, userID)

				reqLogger.Info().Msgf("user %v authorized ", userID)

			}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r.WithContext(ctx))

		})
	}
}
