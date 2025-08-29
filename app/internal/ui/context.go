package ui

import "context"

// Common context keys - use a struct to prevent conflicts
type contextKey struct {
	name string
}

// These context keys should be used by handlers after the RequireAuth middleware has run.
// We use context instead of reading cookies directly because, when tokens are refreshed
// mid-request, the cookies will be stale.
var (
	accessTokenKey = contextKey{"access_token"}
	accountInfoKey = contextKey{"account_info"}
	isnPermsKey    = contextKey{"isn_perms"}
)

func ContextWithAccessToken(ctx context.Context, accessToken string) context.Context {
	return context.WithValue(ctx, accessTokenKey, accessToken)
}

func ContextAccessToken(ctx context.Context) (string, bool) {
	accesToken, ok := ctx.Value(accessTokenKey).(string)
	return accesToken, ok
}

func ContextWithAccountInfo(ctx context.Context, accountInfo AccountInfo) context.Context {
	return context.WithValue(ctx, accountInfoKey, accountInfo)
}

func ContextAccountInfo(ctx context.Context) (AccountInfo, bool) {
	accountInfo, ok := ctx.Value(accountInfoKey).(AccountInfo)
	return accountInfo, ok
}

func ContextWithIsnPerms(ctx context.Context, isnPerms map[string]IsnPerms) context.Context {
	return context.WithValue(ctx, isnPermsKey, isnPerms)
}

func ContextIsnPerms(ctx context.Context) (map[string]IsnPerms, bool) {
	isnPerms, ok := ctx.Value(isnPermsKey).(map[string]IsnPerms)
	return isnPerms, ok
}
