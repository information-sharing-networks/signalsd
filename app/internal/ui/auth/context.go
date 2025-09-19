package auth

import (
	"context"

	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// Common context keys - use a struct to prevent conflicts
type contextKey struct {
	name string
}

var (
	accessTokenKey = contextKey{"access-token"}
	isnPermsKey    = contextKey{"isn-perms"}
	accountInfoKey = contextKey{"account-info"}
)

func ContextWithAccessToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, accessTokenKey, token)
}

func ContextAccessToken(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(accessTokenKey).(string)
	return token, ok
}

func ContextWithIsnPerms(ctx context.Context, perms map[string]types.IsnPerm) context.Context {
	return context.WithValue(ctx, isnPermsKey, perms)
}

func ContextIsnPerms(ctx context.Context) (map[string]types.IsnPerm, bool) {
	perms, ok := ctx.Value(isnPermsKey).(map[string]types.IsnPerm)
	return perms, ok
}

func ContextWithAccountInfo(ctx context.Context, accountInfo *types.AccountInfo) context.Context {
	return context.WithValue(ctx, accountInfoKey, accountInfo)
}

func ContextAccountInfo(ctx context.Context) (*types.AccountInfo, bool) {
	accountInfo, ok := ctx.Value(accountInfoKey).(*types.AccountInfo)
	return accountInfo, ok
}
