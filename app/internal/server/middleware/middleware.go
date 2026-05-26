package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jub0bs/cors"
	"golang.org/x/time/rate"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
)

// CORS returns a CORS middleware using the provided pre-built middleware instance.
func CORS(middleware *cors.Middleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return middleware.Wrap(next)
	}
}

func SecurityHeaders(environment string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			// stil in widespread use
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// for legacy support
			w.Header().Set("X-Frame-Options", "DENY")

			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self' 'unsafe-inline'; frame-ancestors 'none';")

			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			if environment == "prod" || environment == "staging" {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestSizeLimit limits the size of request bodies and adds the limit as a header for client awareness
func RequestSizeLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			w.Header().Set("Signalsd-Max-Request-Size", strconv.FormatInt(maxBytes, 10))

			// Check Content-Length header first (if present)
			if r.ContentLength > maxBytes {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("component", "RequestSizeLimit"),
					slog.Int64("content_length", r.ContentLength),
					slog.Int64("max_bytes", maxBytes),
				)

				errorMsg := fmt.Sprintf("Request body exceeds maximum size of %d bytes", maxBytes)
				responses.RenderError(w, r, &apperrors.HTTPError{
					Status:  http.StatusRequestEntityTooLarge,
					Code:    apperrors.ErrCodeRequestTooLarge,
					Message: errorMsg,
				})
				return
			}

			// Wrap the body reader to enforce the limit (if the body is larger than maxBytes, the error will be picked up in the handler that decodes the request body)
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit limits requests per second. If requestsPerSecond <= 0, rate limiting is disabled.
func RateLimit(requestsPerSecond int32, burst int32) func(http.Handler) http.Handler {
	// If rate limiting is disabled, return a no-op middleware
	if requestsPerSecond <= 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), int(burst))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("component", "RateLimit"),
				)

				responses.RenderError(w, r, &apperrors.HTTPError{
					Status:  http.StatusTooManyRequests,
					Code:    apperrors.ErrCodeRateLimitExceeded,
					Message: "Rate limit exceeded",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
