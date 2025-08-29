package auth

import (
	"context"

	"github.com/google/uuid"
)

// Common context keys - use a struct to prevent conflicts
type contextKey struct {
	name string
}

var (
	accountIDKey          = contextKey{"account-id"}
	accountTypeKey        = contextKey{"account-type"}
	claimsKey             = contextKey{"claims"}
	hashedRefreshTokenKey = contextKey{"hashed_refresh_token"}
)

func ContextWithAccountID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, accountIDKey, id)
}

func ContextAccountID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(accountIDKey).(uuid.UUID)
	return id, ok
}

func ContextWithAccountType(ctx context.Context, accountType string) context.Context {
	return context.WithValue(ctx, accountTypeKey, accountType)
}

func ContextAccountType(ctx context.Context) (string, bool) {
	accountType, ok := ctx.Value(accountTypeKey).(string)
	return accountType, ok
}

func ContextWithHashedRefreshToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, hashedRefreshTokenKey, token)
}

func ContextHashedRefreshToken(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(hashedRefreshTokenKey).(string)
	return token, ok
}

func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

func ContextClaims(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*Claims)
	return claims, ok
}
