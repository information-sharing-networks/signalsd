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

// context keys
type contextKey struct {
	name string
}

var (
	logAttrsKey         = contextKey{"log_attrs"}
	middlewareLoggerKey = contextKey{"middleware_logger"}
)

// ContextWithLogAttr allows handlers to add attributes to the final request log.
//
// The context values are added to a shared slice that is used by the RequestLogging middleware to create the final log message for the http request.
//
// Use this function to add useful tracking information to the final request log, for example the account_id of the authenticated user.
func ContextWithLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if attrPtr, ok := ctx.Value(logAttrsKey).(*[]slog.Attr); ok {
		*attrPtr = append(*attrPtr, attrs...)
		return ctx
	}
	// programming error - this should not happen
	slog.Warn("ContextWithLogAttrs called on context without shared log attributes slice")
	return ctx
}

func ContextLogAttrs(ctx context.Context) []slog.Attr {
	if attrPtr, ok := ctx.Value(logAttrsKey).(*[]slog.Attr); ok {
		return *attrPtr
	}

	// this indicates a programming error
	slog.Warn("ContextLogAttrs called on context without shared log attributes slice")
	return nil
}

// ContextMiddlewareLogger retrieves the request-scoped logger from context.
//
// the logger is used by middleware that needs to create intermediary log messages before the request finsihes.
// the log entries will include the request_id.
func ContextMiddlewareLogger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(middlewareLoggerKey).(*slog.Logger); ok {
		return logger
	}

	// Fallback to default logger if no request logger in context
	slog.Warn("ContextLogger called on context without request logger - using default logger")
	return slog.Default()
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

/*
This package implements a two types of logging:

1. immediate middleware logging (contextMiddlewareLogger):
   - Use for events that occur during request processing

2. request completion logging (ContextWithLogAttrs):
   - Use for attributes that should appear in the final HTTP request log
*/

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
			case strings.HasPrefix(r.URL.Path, "/docs"):
				component = "docs"
			case strings.HasPrefix(r.URL.Path, "/swagger.json"):
				component = "docs"
			case strings.HasPrefix(r.URL.Path, "/api/"):
				component = "api"
			case strings.HasPrefix(r.URL.Path, "/oauth/"):
				component = "oauth"
			default:
				component = "ui"
			}

			// Create request-scoped logger with common fields
			middlewareLogger := logger.With(
				slog.String("type", "middleware"),
				slog.String("request_id", requestID),
			)

			// shared slice for attributes that handlers can modify
			sharedAttrs := &[]slog.Attr{}

			// Store shared attributes in context
			ctx := context.WithValue(r.Context(), logAttrsKey, sharedAttrs)

			// Store request-scoped logger in context for middleware use
			ctx = context.WithValue(ctx, middlewareLoggerKey, middlewareLogger)

			req := r.WithContext(ctx)

			// Wrap response writer to capture status
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// pass on updated writer and request
			next.ServeHTTP(ww, req)

			duration := time.Since(start)

			logAttrs := []slog.Attr{
				slog.String("type", "HTTP"),
				slog.Int("status", ww.Status()),
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("component", component),
			}

			// Add any attributes that were added to the context during request processing
			contextAttrs := ContextLogAttrs(req.Context())
			if len(contextAttrs) > 0 {
				logAttrs = append(logAttrs, contextAttrs...)
			}

			// Add duration and bytes written to response
			logAttrs = append(logAttrs,
				slog.Duration("duration", duration),
				slog.Int("bytes", ww.BytesWritten()),
			)

			// Log request completion with all attributes
			switch {
			case ww.Status() >= 500:
				logger.LogAttrs(r.Context(), slog.LevelError, "request completed", logAttrs...)
			case ww.Status() >= 400:
				logger.LogAttrs(r.Context(), slog.LevelWarn, "request completed", logAttrs...)
			default:
				logger.LogAttrs(r.Context(), slog.LevelInfo, "request completed", logAttrs...)
			}
		})
	}
}
