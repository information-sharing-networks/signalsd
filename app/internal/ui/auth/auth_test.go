package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// signToken returns an HS256 token with the supplied claims.
// CheckAccessTokenStatus does not verify signatures, so the key is arbitrary.
func signToken(t *testing.T, claims jwt.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-key"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return token
}

func TestCheckAccessTokenStatus(t *testing.T) {
	a := &AuthService{}

	tests := []struct {
		name    string
		details *types.AccessTokenDetails
		want    AccessTokenStatus
	}{
		{
			name:    "nil details",
			details: nil,
			want:    TokenMissing,
		},
		{
			name:    "empty token",
			details: &types.AccessTokenDetails{AccessToken: ""},
			want:    TokenInvalid,
		},
		{
			name:    "malformed token",
			details: &types.AccessTokenDetails{AccessToken: "not-a-jwt"},
			want:    TokenInvalid,
		},
		{
			// the cookie is client-supplied, so a token without an exp claim must not cause a nil pointer dereference
			name: "token without exp claim",
			details: &types.AccessTokenDetails{AccessToken: signToken(t, jwt.RegisteredClaims{
				Subject: "test",
			})},
			want: TokenInvalid,
		},
		{
			name: "expired token",
			details: &types.AccessTokenDetails{AccessToken: signToken(t, jwt.RegisteredClaims{
				Subject:   "test",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			})},
			want: TokenExpired,
		},
		{
			name: "valid token",
			details: &types.AccessTokenDetails{AccessToken: signToken(t, jwt.RegisteredClaims{
				Subject:   "test",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			})},
			want: TokenValid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.CheckAccessTokenStatus(tt.details); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
