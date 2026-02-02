// Package logger provides structured logging for signalsd with three distinct logging patterns:
//
//  1. **Application-level logging**: Use the logger returned by InitLogger() for server startup,
//     shutdown, configuration, and other non-HTTP operations.
//     Example: appLogger.Info("Starting server", slog.String("version", version))
//
//  2. **HTTP request completion logging**: Automatic logging via RequestLogging middleware.
//     This logs when HTTP requests complete with status, duration, etc. No manual calls needed.
//
//  3. **Request-scoped adhoc logging**: Use ContextRequestLogger(r.Context()) for events
//     that occur during request processing. These logs include the request ID for tracing.
//     Example: reqLogger.Error("Database query failed", slog.String("error", err.Error()))
//
// All request-scoped logs (patterns 2 and 3) automatically include request IDs for tracing.
// This pattern works in both standalone UI mode and integrated signalsd mode.
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
	logAttrsKey      = contextKey{"log_attrs"}
	RequestLoggerKey = contextKey{"request_logger"}
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

// ContextRequestLogger retrieves the request-scoped logger from context.
//
// the logger can be used to create intermediary log messages before the request finsihes.
//
// To add attributes to the final request log, use ContextWithLogAttrs instead.
func ContextRequestLogger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(RequestLoggerKey).(*slog.Logger); ok {
		return logger
	}

	// Fallback to default logger if no request logger in context
	slog.Default().Warn("ContextLogger called on context without request logger - using default logger")
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
	case "none":
		return 100
	default:
		return slog.LevelDebug
	}
}

// InitLogger creates a logger with the specified log level.
// Uses colorized text format for dev environment otherwise output is JSON
func InitLogger(logLevel slog.Level, environment string) *slog.Logger {
	if environment == "dev" || environment == "test" {
		return slog.New(
			tint.NewHandler(os.Stderr, &tint.Options{
				Level:      logLevel,
				TimeFormat: time.Kitchen,
			}),
		)
	} else {
		return slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
				Level: logLevel,
			}))
	}

}

// RequestLogging is a middleware that logs HTTP request completion.
//
// By default the log messages include the request_id, request path, method, status, duration and bytes written.
// Handlers can use ContextWithLogAttrs to add additional attributes to the log message.
//
// To create log messages about events that occur during a request, use the ContextRequestLogger.
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

			// Determine request req_type based on path
			var req_type string
			switch {
			case strings.HasPrefix(r.URL.Path, "/docs"), strings.HasPrefix(r.URL.Path, "/swagger.json"):
				req_type = "docs"
			case strings.HasPrefix(r.URL.Path, "/api/"):
				req_type = "api"
			case strings.HasPrefix(r.URL.Path, "/oauth/"):
				req_type = "oauth"
			case strings.HasPrefix(r.URL.Path, "/ui-api/"): // ui routes used when rendering ui components
				req_type = "ui-api"
			default: // ui end-user routes (/login /register etc)
				req_type = "ui-client"
			}

			// shared slice for attributes that handlers can modify
			sharedAttrs := &[]slog.Attr{}

			ctx := context.WithValue(r.Context(), logAttrsKey, sharedAttrs)

			// Create request-scoped logger with common fields
			reqLogger := logger.With(
				slog.String("type", "request_log"),
				slog.String("request_id", requestID),
			)

			// Store request-scoped logger in context for adhoc logging outside of http request completion logs
			ctx = context.WithValue(ctx, RequestLoggerKey, reqLogger)

			req := r.WithContext(ctx)

			// Wrap response writer to capture status
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, req)

			duration := time.Since(start)

			// add standard http status attributes to log message
			logAttrs := []slog.Attr{
				slog.String("type", "HTTP"),
				slog.Int("status", ww.Status()),
				slog.String("req_type", req_type),
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
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
