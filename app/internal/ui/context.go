package ui

import "context"

// Common context keys - use a struct to prevent conflicts
type contextKey struct {
	name string
}

var (
	accessTokenKey = contextKey{"access_token"}
)

func ContextWithAccessToken(ctx context.Context, accessToken string) context.Context {
	return context.WithValue(ctx, accessTokenKey, accessToken)
}

func ContextAccessToken(ctx context.Context) (string, bool) {
	accesToken, ok := ctx.Value(accessTokenKey).(string)
	return accesToken, ok
}
