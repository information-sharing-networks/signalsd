package auth

import (
	"context"

	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// Common context keys - use a struct to prevent conflicts
type contextKey struct {
	name string
}

var accessTokenDetailsKey = contextKey{"access-token-details"}

func ContextWithAccessTokenDetails(ctx context.Context, accessTokenDetails *types.AccessTokenDetails) context.Context {
	return context.WithValue(ctx, accessTokenDetailsKey, accessTokenDetails)
}

func ContextAccessTokenDetails(ctx context.Context) (*types.AccessTokenDetails, bool) {
	accessTokenDetails, ok := ctx.Value(accessTokenDetailsKey).(*types.AccessTokenDetails)
	return accessTokenDetails, ok
}
