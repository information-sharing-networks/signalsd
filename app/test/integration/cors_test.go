//go:build integration

package integration

// CORS tests
// Origin validation and enforcement
// Public endpoints allow all origins, the other endpoints do not allow cross-origin requests
import (
	"net/http"
	"testing"
)

// checkOriginIsAllowed checks if the given origin is allowed for the given endpoint
func checkOriginIsAllowed(t *testing.T, endpoint, origin string) (bool, string) {
	t.Helper()

	// make a preflight request with an Origin header and check the Access-Control-Allow-Origin response header
	req, err := http.NewRequest("OPTIONS", endpoint, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// the returned Access-Control-Allow-Origin header only contains the origin if it is allowed
	corsOrigin := resp.Header.Get("Access-Control-Allow-Origin")

	// all origins allowed
	if corsOrigin == "*" {
		return true, corsOrigin
	}

	return corsOrigin == origin, corsOrigin
}

// TestCORS tests that the public endpoints allow cross-origin requests from any origin and that
// the other endpoints do not allow cross-origin requests (browser-based clients must call them from their own backend)
func TestCORS(t *testing.T) {
	origin := "https://any-site.example.com"

	testEnv := startInProcessServer(t, "")

	privateEndpoints := []string{
		testEnv.baseURL + "/api/accounts",
		testEnv.baseURL + "/api/isn/sample-isn/signal-types/sample-signal-type/v1.0.0/signals/search",
		testEnv.baseURL + "/oauth/token",
	}
	publicEndpoints := []string{
		testEnv.baseURL + "/health/live",
		testEnv.baseURL + "/api/public/isn/sample-isn/signal-types/sample-signal-type/v1.0.0/signals/search",
	}

	for _, endpoint := range privateEndpoints {
		t.Run("cross-origin blocked "+endpoint, func(t *testing.T) {
			allowed, returnedOrigin := checkOriginIsAllowed(t, endpoint, origin)
			if allowed {
				t.Errorf("Expected origin %s to be blocked, but got Access-Control-Allow-Origin: %s", origin, returnedOrigin)
			}
		})
	}

	for _, endpoint := range publicEndpoints {
		t.Run("cross-origin allowed "+endpoint, func(t *testing.T) {
			allowed, returnedOrigin := checkOriginIsAllowed(t, endpoint, origin)
			if !allowed {
				t.Errorf("Expected origin %s to be allowed, but got Access-Control-Allow-Origin: %s", origin, returnedOrigin)
			}
		})
	}
}

// TestCrossOriginProtection tests that the server rejects POST requests that the browser reports as coming from another origin
// (CSRF protection for the cookie-authenticated routes - the protection is a global middleware, so a single endpoint is tested)
func TestCrossOriginProtection(t *testing.T) {
	testEnv := startInProcessServer(t, "")

	req, err := http.NewRequest(http.MethodPost, testEnv.baseURL+"/oauth/token?grant_type=refresh_token", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Sec-Fetch-Site", "same-site") // e.g. a sibling subdomain, which SameSite=Strict does not block
	req.Header.Set("Origin", "https://other.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, resp.StatusCode)
	}
}
