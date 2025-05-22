package context

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Common context keys - use a struct to prevent conflicts
type contextKey struct {
	name string
}

var (
	requestLogger = contextKey{"request-logger"}
	userAccountID = contextKey{"user-id"}
)

func WithUserAccountID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userAccountID, id)
}

func UserAccountID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userAccountID).(uuid.UUID)
	return id, ok
}

func WithRequestLogger(ctx context.Context, logger *zerolog.Logger) context.Context {
	return context.WithValue(ctx, requestLogger, logger)
}

func RequestLogger(ctx context.Context) (*zerolog.Logger, bool) {
	id, ok := ctx.Value(requestLogger).(*zerolog.Logger)
	return id, ok
}
