package logger

import (
	"context"

	"github.com/rs/zerolog"
)

// Common context keys - use a struct to prevent conflicts
type contextKey struct {
	name string
}

var requestLogger = contextKey{"request-logger"}

func ContextWithRequestLogger(ctx context.Context, logger *zerolog.Logger) context.Context {
	return context.WithValue(ctx, requestLogger, logger)
}

func ContextRequestLogger(ctx context.Context) (*zerolog.Logger, bool) {
	logger, ok := ctx.Value(requestLogger).(*zerolog.Logger)
	return logger, ok
}
