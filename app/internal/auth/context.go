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
	userAccountIDKey      = contextKey{"user-id"}
	accessTokenClaimsKey  = contextKey{"access-token-claims"}
	hashedRefreshTokenKey = contextKey{"hashed_refersh_token"}
)

func ContextWithUserAccountID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userAccountIDKey, id)
}

func ContextUserAccountID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userAccountIDKey).(uuid.UUID)
	return id, ok
}

func ContextWithHashedRefreshToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, hashedRefreshTokenKey, token)
}

func ContextHashedRefreshToken(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(hashedRefreshTokenKey).(string)
	return token, ok
}

func ContextWithAccessTokenClaims(ctx context.Context, claims *AccessTokenClaims) context.Context {
	return context.WithValue(ctx, accessTokenClaimsKey, claims)
}

func ContextAccessTokenClaims(ctx context.Context) (*AccessTokenClaims, bool) {
	claims, ok := ctx.Value(accessTokenClaimsKey).(*AccessTokenClaims)
	return claims, ok
}
