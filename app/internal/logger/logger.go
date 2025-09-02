package logger

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/lmittmann/tint"
)

// logger is passed to handlers and authservice using context
type contextKey struct {
	name string
}

var (
	loggerContextKey = contextKey{"logger"}
)

func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey, logger)
}

func ContextLogger(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(loggerContextKey).(*slog.Logger)
	if !ok {
		// fallback
		return slog.Default()
	}
	return logger
}

// ParseLogLevel converts a string log level to slog.Level
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug // default to debug
	}
}

// InitLogger creates a logger with the specified log level.
// Uses text format for dev environment otherwise output is JSON
func InitLogger(logLevel slog.Level, environment string) *slog.Logger {
	if environment == "dev" {
		// Use colourized text handler for development
		return slog.New(
			tint.NewHandler(os.Stderr, &tint.Options{
				Level:      logLevel,
				TimeFormat: time.Kitchen,
			}),
		)
	} else {
		// Use JSON handler for production
		return slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
				Level: logLevel,
			}))
	}

}

// RequestLogging is a middleware that logs HTTP requests
func RequestLogging(logger *slog.Logger) func(http.Handler) http.Handler {
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
			reqLogger := logger.With(
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("component", component),
			)
			ctx := ContextWithLogger(r.Context(), reqLogger)

			// Wrap response writer to capture status
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r.WithContext(ctx))

			duration := time.Since(start)

			// Log request completion with duration and bytes written
			switch {
			case ww.Status() >= 500:
				reqLogger.Error("Request failed with server error",
					slog.Int("status", ww.Status()),
					slog.Duration("duration", duration),
					slog.Int("bytes", ww.BytesWritten()))
			case ww.Status() >= 400:
				reqLogger.Warn("Request failed with client error",
					slog.Int("status", ww.Status()),
					slog.Duration("duration", duration),
					slog.Int("bytes", ww.BytesWritten()))
			default:
				reqLogger.Info("Request completed successfully",
					slog.Int("status", ww.Status()),
					slog.Duration("duration", duration),
					slog.Int("bytes", ww.BytesWritten()))
			}
		})
	}
}
