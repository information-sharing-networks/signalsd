package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/nickabs/signalsd/app/internal/context"
	"github.com/rs/zerolog/log"
)

// standard logger for all http requests
func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		reqLogger := log.With().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Logger()

		ctx := context.WithRequestLogger(r.Context(), &reqLogger)

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
