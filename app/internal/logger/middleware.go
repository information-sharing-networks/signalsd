package logger

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// standard logger for all http requests
func LoggingMiddleware(logger *zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			// Skip logging for health requests
			if strings.HasPrefix(r.URL.Path, "/health/") {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			requestID := middleware.GetReqID(r.Context())

			// Create request-scoped logger with common fields
			reqLogger := logger.With().
				Str("request_id", requestID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
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
				Dur("duration", duration).
				Int("bytes", ww.BytesWritten()).
				Msg("Request completed")
		})
	}
}
