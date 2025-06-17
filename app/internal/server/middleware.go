package server

import (
	"fmt"
	"net/http"
	"strconv"

	"golang.org/x/time/rate"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
)

// CORS adds CORS headers to control which sites are allowed to use the service.
//
// Set via ALLOWED_ORIGINS environment variable (defaults to "*" if not set).
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// If no origins specified, allow all
			if len(allowedOrigins) == 0 || allowedOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			} else {
				// Check if origin is in allowed list
				allowedOrigin := ""
				for _, o := range allowedOrigins {
					if o == origin || o == "*" {
						allowedOrigin = o
						break
					}
				}

				if allowedOrigin != "" {
					w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(86400))
				}
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders adds security-related headers to all responses
func SecurityHeaders(environment string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			if environment == "prod" {
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

			w.Header().Set("X-Max-Request-Size", strconv.FormatInt(maxBytes, 10))

			// Check Content-Length header first (if present)
			if r.ContentLength > maxBytes {
				errorMsg := fmt.Sprintf("Request body exceeds maximum size of %d bytes", maxBytes)
				responses.RespondWithError(w, r, http.StatusRequestEntityTooLarge,
					apperrors.ErrCodeRequestTooLarge, errorMsg)
				return
			}

			// Wrap the body reader to enforce the limit (if the body is larger than maxBytes, the error will be picked up in the handler that decodes the request body)
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit limits requests per second
func RateLimit(requestsPerSecond int, burst int) func(http.Handler) http.Handler {
	limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				responses.RespondWithError(w, r, http.StatusTooManyRequests,
					apperrors.ErrCodeRateLimitExceeded, "Rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
