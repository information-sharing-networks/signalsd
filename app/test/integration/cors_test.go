//go:build integration

package integration

// CORS tests
// Origin validation and enforcement
// Public vs protected endpoint policies
import (
	"net/http"
	"testing"
)

// checkOriginIsAllowed checks if the given origin is allowed for the given endpoint
func checkOriginIsAllowed(t *testing.T, endpoint, origin string) (bool, string) {
	t.Helper()

	// make a preflight request with an Origin header and check he Access-Control-Allow-Origin response header
	req, err := http.NewRequest("OPTIONS", endpoint, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "GET")

	client := &http.Client{}
	resp, err := client.Do(req)
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

// TestCORS tests that protected endpoints respect ALLOWED_ORIGINS configuration and that untrusted origins are blocked
func TestCORS(t *testing.T) {
	// Configure specific allowed origin
	allowedOrigins := "https://trusted-app.example.com|https://trusted-app2.example.com"
	allowedOrigin1 := "https://trusted-app.example.com"
	allowedOrigin2 := "https://trusted-app2.example.com"
	disallowedOrigin := "https://malicious-site.com"

	t.Setenv("ALLOWED_ORIGINS", allowedOrigins)

	// Start server
	testEnv := startInProcessServer(t, "")
	defer testEnv.shutdown()

	privateEndpoint := testEnv.baseURL + "/api/accounts"
	publicEndpoint := testEnv.baseURL + "/health/live"

	t.Run("trusted origin allowed", func(t *testing.T) {
		allowed, returnedOrigin := checkOriginIsAllowed(t, privateEndpoint, allowedOrigin1)
		if !allowed {
			t.Errorf("Expected origin %s to be allowed, but got Access-Control-Allow-Origin: %s", allowedOrigin1, returnedOrigin)
		}
		allowed, returnedOrigin = checkOriginIsAllowed(t, privateEndpoint, allowedOrigin2)
		if !allowed {
			t.Errorf("Expected origin %s to be allowed, but got Access-Control-Allow-Origin: %s", allowedOrigin2, returnedOrigin)
		}
	})

	t.Run("untrusted origin blocked", func(t *testing.T) {
		// Test untrusted origin is blocked
		allowed, returnedOrigin := checkOriginIsAllowed(t, privateEndpoint, disallowedOrigin)
		if allowed {
			t.Errorf("Expected origin %s to be blocked, but got Access-Control-Allow-Origin: %s", disallowedOrigin, returnedOrigin)
		}
	})

	// publics endpoints can be used by everyone, even badies
	t.Run("public endpoint allows all origins", func(t *testing.T) {
		allowed, returnedOrigin := checkOriginIsAllowed(t, publicEndpoint, disallowedOrigin)
		if !allowed {
			t.Errorf("Expected origin %s to be allowed to use publc endpoint, but got Access-Control-Allow-Origin: %s", disallowedOrigin, returnedOrigin)
		}
	})
}
