package logger

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// RequestLogging returns a middleware that logs HTTP requests
func RequestLogging(logger *zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging for health requests
			if strings.HasPrefix(r.URL.Path, "/health/") {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			requestID := middleware.GetReqID(r.Context())

			// Determine request component based on path
			var component string
			switch {
			case strings.HasPrefix(r.URL.Path, "/api/"):
				component = "api"
			case strings.HasPrefix(r.URL.Path, "/oauth/"):
				component = "oauth"
			case strings.HasPrefix(r.URL.Path, "/admin/"):
				component = "admin"
			default:
				component = "ui"
			}

			// Create request-scoped logger with common fields
			reqLogger := logger.With().
				Str("request_id", requestID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Str("component", component).
				Logger()

			ctx := reqLogger.WithContext(r.Context())

			// Wrap response writer to capture status
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r.WithContext(ctx))

			duration := time.Since(start)

			var logEvent *zerolog.Event
			switch {
			case ww.Status() >= 500:
				logEvent = reqLogger.Error()
			case ww.Status() >= 400:
				logEvent = reqLogger.Warn()
			default:
				logEvent = reqLogger.Info()
			}

			logEvent.
				Int("status", ww.Status()).
				Str("duration", duration.String()).
				Int("bytes", ww.BytesWritten()).
				Msg("Request completed")
		})
	}
}
