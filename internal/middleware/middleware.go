package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/auth"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
	"github.com/rs/zerolog/log"
)

// @LoggerMiddleware godoc
//
//	standard logger for all http requests
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

// AuthorizationMiddleware godoc
//
//	this middleware sets a contextKey with the userID where authorized endpoints are handled
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

			// by default all requests are authenticated using an access token.  Exceptions are in the case statement below
			switch {
			case r.Method == http.MethodGet:
				reqLogger.Info().Msg("GET request, skipping authorization check")

			case r.Method == http.MethodPost && r.URL.Path == "/api/login":
				reqLogger.Info().Msg("login, skipping authorization check")

			case r.Method == http.MethodPost && r.URL.Path == "/api/refresh":
				reqLogger.Info().Msg("refresh access code, skipping access token authorization check (refresh token checked instead)") // todo middleware

			case r.Method == http.MethodPost && r.URL.Path == "/api/revoke":
				reqLogger.Info().Msg("revoke refresh code, skipping authorization check")

			case r.Method == http.MethodPost && r.URL.Path == "/api/users":
				reqLogger.Info().Msg("creating user, skipping authorization check")

			// reset is unprotected but will only work on dev
			case r.Method == http.MethodPost && r.URL.Path == "/admin/reset":
				reqLogger.Info().Msg("reset database, skipping authorization check")

			default:
				userID, err := authService.CheckAccessTokenAuthorization(r.Header)
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

// GetWithSignalDefID godoc
//
//	signal admin tasks can be done using either signal def id or slug/version path param
//	this function sets a context value with the id so generic handlers can be used
func GetWithSignalDefID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			signalDefIDString := r.PathValue("signal_def_id")

			signalDefID, err := uuid.Parse(signalDefIDString)
			if err != nil {
				helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, fmt.Sprintf("Invalid signal definition ID: %v", err))
				return
			}

			ctx := context.WithValue(r.Context(), signals.SignalDefIDKey, signalDefID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetWithSignalDefSlug godoc
//
//	signal admin tasks can be done using either signal def id or slug/version path param
//	this function sets a context value with the id so generic handlers can be used
func GetWithSignalDefSlug(cfg *signals.ServiceConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			slug := r.PathValue("slug")
			semVer := r.PathValue("sem_ver")

			res, err := cfg.DB.GetSignalDefBySlug(r.Context(), database.GetSignalDefBySlugParams{
				Slug:   slug,
				SemVer: semVer,
			})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeResourceNotFound, fmt.Sprintf("No signal definition found for %s/v%s", slug, semVer))
					return
				}
				helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the signal definition from the database %v", err))
				return
			}
			ctx := context.WithValue(r.Context(), signals.SignalDefIDKey, res.ID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}

	// Check common reverse proxy headers
	if scheme := r.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "http"
}
