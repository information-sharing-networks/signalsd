package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

func newTestStandaloneServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		Environment:             "test",
		Host:                    "127.0.0.1",
		Port:                    3000,
		ReadTimeout:             15 * time.Second,
		WriteTimeout:            15 * time.Second,
		IdleTimeout:             65 * time.Second,
		DocumentTransferTimeout: 2 * time.Minute,
		APIBaseURL:              "http://localhost:8080",
		TrustedProxies:          1,
	}
	return NewStandaloneServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func makeAuthCookie(t *testing.T, role string, isnPerms map[string]types.IsnPerm) *http.Cookie {
	t.Helper()

	type testClaims struct {
		jwt.RegisteredClaims
		AccountID   string                   `json:"account_id"`
		AccountType string                   `json:"account_type"`
		Role        string                   `json:"role"`
		IsnPerms    map[string]types.IsnPerm `json:"isn_perms,omitempty"`
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, testClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "acc-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		AccountID:   "acc-1",
		AccountType: "user",
		Role:        role,
		IsnPerms:    isnPerms,
	}).SignedString([]byte("test-key"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	payload, err := json.Marshal(struct {
		AccessToken string `json:"access_token"`
		Email       string `json:"email"`
	}{
		AccessToken: token,
		Email:       "user@example.com",
	})
	if err != nil {
		t.Fatalf("failed to marshal cookie payload: %v", err)
	}

	return &http.Cookie{
		Name:  config.AccessTokenDetailsCookieName,
		Value: base64.StdEncoding.EncodeToString(payload),
	}
}

func TestLimitRequestBody(t *testing.T) {
	var parseErr error
	h := limitRequestBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parseErr = r.ParseForm()
	}))

	body := "email=" + strings.Repeat("a", maxRequestBodySize)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	var maxBytesErr *http.MaxBytesError
	if !errors.As(parseErr, &maxBytesErr) {
		t.Errorf("expected ParseForm to fail with *http.MaxBytesError, got %v", parseErr)
	}
}

func TestStandaloneSecurityHeaders(t *testing.T) {
	srv := newTestStandaloneServer(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected X-Content-Type-Options=nosniff, got %q", rec.Header().Get("X-Content-Type-Options"))
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("expected X-Frame-Options=DENY, got %q", rec.Header().Get("X-Frame-Options"))
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("expected Content-Security-Policy header to be set")
	}
}

func TestHomePageRedirects(t *testing.T) {
	srv := newTestStandaloneServer(t)

	t.Run("unauthenticated redirects to login", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Errorf("expected 303 -> /login, got %d -> %q", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("authenticated redirects to dashboard", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(makeAuthCookie(t, "member", nil))
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/dashboard" {
			t.Errorf("expected 303 -> /dashboard, got %d -> %q", rec.Code, rec.Header().Get("Location"))
		}
	})
}

func TestDownloadDocumentExtendsRequestTimeout(t *testing.T) {
	// the API responds after the normal request timeout (WriteTimeout - 1s) has passed,
	// so the download only succeeds if ExtendRequestTimeout is applied to the route
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="test.pdf"`)
		_, _ = w.Write([]byte("%PDF-1.4"))
	}))
	defer api.Close()

	cfg := &config.Config{
		Environment:             "test",
		Host:                    "127.0.0.1",
		Port:                    3000,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            2 * time.Second, // RequestTimeout gives 1s
		IdleTimeout:             65 * time.Second,
		DocumentTransferTimeout: 2 * time.Minute,
		APIBaseURL:              api.URL,
		TrustedProxies:          1,
	}
	srv := NewStandaloneServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/ui-api/isn/sample-isn/signal-types/doc/v1.0.0/signals/01950000-0000-7000-8000-000000000001/content", nil)
	req.AddCookie(makeAuthCookie(t, "member", map[string]types.IsnPerm{
		"sample-isn": {CanRead: true, Visibility: "private", InUse: true},
	}))
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from UI download, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "%PDF-1.4" {
		t.Errorf("unexpected body %q", rec.Body.String())
	}
}
