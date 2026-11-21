package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// signToken returns an HS256 token with the supplied claims.
// ParseAuthCookie does not verify signatures, so the key is arbitrary.
func signToken(t *testing.T, claims jwt.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-key"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return token
}

func TestSetAndParseAuthCookie(t *testing.T) {
	a := &AuthService{environment: "dev"}

	// the JWT claims hold only in_use for each signal type
	token := signToken(t, accessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * time.Minute))},
		AccountID:        "acc-123",
		AccountType:      "user",
		Role:             "isnadmin",
		IsnPerms: map[string]types.IsnPerm{
			"sample-isn": {
				CanRead:     true,
				Visibility:  "private",
				InUse:       true,
				SignalTypes: map[string]types.SignalType{"sample-signal/v1.2.3": {InUse: true}},
			},
		},
	})

	// the login API response repeats the permissions with the full signal type details
	details := &types.AccessTokenDetails{
		AccessToken: token,
		ExpiresIn:   1800,
		Email:       "user@example.com",
		IsnPerms: map[string]types.IsnPerm{
			"sample-isn": {SignalTypes: map[string]types.SignalType{
				"sample-signal/v1.2.3": {SchemaURL: "https://github.com/org/repo/blob/main/schema.json"},
			}},
		},
	}

	rec := httptest.NewRecorder()
	if err := a.SetAuthCookies(rec, details, &http.Cookie{Name: config.RefreshTokenCookieName, Value: "refresh-val"}); err != nil {
		t.Fatalf("SetAuthCookies() error = %v", err)
	}

	var authCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == config.AccessTokenDetailsCookieName {
			authCookie = c
		}
	}
	if authCookie == nil {
		t.Fatalf("expected %s cookie to be set", config.AccessTokenDetailsCookieName)
	}

	rawJSON, err := base64.StdEncoding.DecodeString(authCookie.Value)
	if err != nil {
		t.Fatalf("cookie is not valid base64: %v", err)
	}
	if strings.Contains(string(rawJSON), "isn_perms") {
		t.Errorf("expected cookie without the duplicated isn_perms, got: %s", rawJSON)
	}

	parsed, status, err := a.ParseAuthCookie(authCookie.Value)
	if err != nil || status != TokenValid {
		t.Fatalf("ParseAuthCookie() status = %v, err = %v, want TokenValid", status, err)
	}
	if parsed.AccountID != "acc-123" || parsed.Email != "user@example.com" || parsed.Role != "isnadmin" || parsed.AccountType != "user" {
		t.Errorf("unexpected parsed identity: %+v", parsed)
	}

	perm := parsed.IsnPerms["sample-isn"]
	if !perm.CanRead || perm.Visibility != "private" || !perm.InUse {
		t.Errorf("unexpected parsed IsnPerm: %+v", perm)
	}
	st := perm.SignalTypes["sample-signal/v1.2.3"]
	if st.Path != "sample-signal/v1.2.3" || st.Slug != "sample-signal" || st.SemVer != "1.2.3" || !st.InUse {
		t.Errorf("unexpected parsed SignalType: %+v", st)
	}
}

func TestParseAuthCookieEdgeCases(t *testing.T) {
	a := &AuthService{environment: "dev"}

	encodePayload := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	tokenCookie := func(claims jwt.Claims) string {
		return encodePayload(authCookiePayload{AccessToken: signToken(t, claims), Email: "a@b.com"})
	}

	tests := []struct {
		name       string
		cookie     string
		wantStatus AccessTokenStatus
		wantErr    bool
	}{
		{name: "invalid base64", cookie: "!!!not-base64!!!", wantStatus: TokenInvalid, wantErr: true},
		{name: "invalid json", cookie: base64.StdEncoding.EncodeToString([]byte("{bad")), wantStatus: TokenInvalid, wantErr: true},
		{name: "empty access token", cookie: encodePayload(authCookiePayload{Email: "a@b.com"}), wantStatus: TokenInvalid},
		{name: "malformed token", cookie: encodePayload(authCookiePayload{AccessToken: "not-a-jwt"}), wantStatus: TokenInvalid},
		{
			// the cookie is client-supplied, so a token without an exp claim must not cause a nil pointer dereference
			name:       "token without exp",
			cookie:     tokenCookie(jwt.RegisteredClaims{Subject: "acc-1"}),
			wantStatus: TokenInvalid,
		},
		{
			name:       "expired token",
			cookie:     tokenCookie(jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}),
			wantStatus: TokenExpired,
		},
		{
			// cookies written before the slim format still decode, and the role comes from the JWT rather than the cookie
			name: "legacy verbose cookie format",
			cookie: encodePayload(types.AccessTokenDetails{
				AccessToken: signToken(t, accessTokenClaims{
					RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
					Role:             "member",
				}),
				Email: "legacy@example.com",
				Role:  "forged-role-ignored",
			}),
			wantStatus: TokenValid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details, status, err := a.ParseAuthCookie(tt.cookie)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAuthCookie() err = %v, wantErr %v", err, tt.wantErr)
			}
			if status != tt.wantStatus {
				t.Errorf("ParseAuthCookie() status = %v, want %v", status, tt.wantStatus)
			}
			if tt.name == "legacy verbose cookie format" && (details == nil || details.Role != "member" || details.Email != "legacy@example.com") {
				t.Errorf("expected role from JWT (member) and email from cookie, got %+v", details)
			}
		})
	}
}
