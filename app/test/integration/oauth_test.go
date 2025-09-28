//go:build integration

// OAuth endpoint integration tests
// Tests the HTTP-level OAuth flows including middleware routing and request/response handling
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

const testTolerance = 30 * time.Second // use this when checking that token expiriy dates are in the expected range

// TestOAuthTokenEndpoint tests OAuth token generation including:
// - Client credentials grant (service accounts)
// - Refresh token grant (web users) with cookie rotation
// - Invalid credential handling and error responses
func TestOAuthTokenEndpoint(t *testing.T) {

	// set up test env
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// Start server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL, "")
	defer stopServer()

	t.Log("Creating test data...")

	_ = createTestUserWithPassword(t, ctx, testEnv.queries, testEnv.authService, "member", "user@oauth.test", "password123")

	serviceAccount := createTestAccount(t, ctx, testEnv.queries, "member", "service_account", "service@oauth.test")

	clientSecret, err := testEnv.authService.GenerateSecureToken(32)
	if err != nil {
		t.Fatalf("Failed to generate client secret: %v", err)
	}

	hashedSecret := testEnv.authService.HashToken(clientSecret)
	_, err = testEnv.queries.CreateClientSecret(ctx, database.CreateClientSecretParams{
		ServiceAccountAccountID: serviceAccount.ID,
		HashedSecret:            hashedSecret,
		ExpiresAt:               time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Failed to create client secret: %v", err)
	}

	// Get client_id for service account
	serviceAccountDetails, err := testEnv.queries.GetServiceAccountByAccountID(ctx, serviceAccount.ID)
	if err != nil {
		t.Fatalf("Failed to get service account details: %v", err)
	}

	t.Run("client_credentials grant", func(t *testing.T) {
		tests := []struct {
			name           string
			clientID       string
			clientSecret   string
			expectedStatus int
			expectError    bool
		}{
			{
				name:           "valid_credentials",
				clientID:       serviceAccountDetails.ClientID,
				clientSecret:   clientSecret,
				expectedStatus: http.StatusOK,
				expectError:    false,
			},
			{
				name:           "invalid_client_secret",
				clientID:       serviceAccountDetails.ClientID,
				clientSecret:   "wrong-secret",
				expectedStatus: http.StatusUnauthorized,
				expectError:    true,
			},
			{
				name:           "invalid_client_id",
				clientID:       "wrong-client-id",
				clientSecret:   clientSecret,
				expectedStatus: http.StatusUnauthorized,
				expectError:    true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				payload := map[string]string{
					"client_id":     tt.clientID,
					"client_secret": tt.clientSecret,
				}

				response := makeOAuthTokenRequest(t, baseURL, "client_credentials", payload, "")
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Errorf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
					return
				}

				var responseBody map[string]any
				if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if tt.expectError {
					if _, hasErrorCode := responseBody["error_code"]; !hasErrorCode {
						t.Error("Expected error_code in error response")
					}
					return
				}
				if _, ok := responseBody["access_token"]; !ok {
					t.Error("Expected access_token in response")
				}
				if tokenType, ok := responseBody["token_type"].(string); !ok || tokenType != "Bearer" {
					t.Errorf("Expected token_type 'Bearer', got %v", responseBody["token_type"])
				}

			})
		}
	})

	// test refresh token logic (web users)
	t.Run("refresh_token grant", func(t *testing.T) {
		// First login to get a refresh token
		loginPayload := map[string]string{
			"email":    "user@oauth.test",
			"password": "password123",
		}
		loginResponse := makeLoginRequest(t, baseURL, loginPayload)
		defer loginResponse.Body.Close()

		if loginResponse.StatusCode != http.StatusOK {
			t.Fatalf("Login failed: %d", loginResponse.StatusCode)
		}

		// Get the access token (required by the refresh token tests)
		var loginResponseBody map[string]any
		if err := json.NewDecoder(loginResponse.Body).Decode(&loginResponseBody); err != nil {
			t.Fatalf("Failed to decode login response: %v", err)
		}

		accessToken := loginResponseBody["access_token"].(string)
		if accessToken == "" {
			t.Fatal("Failed to get access token from login response")
		}

		// Extract refresh token cookie
		var originaRefreshTokenCookie *http.Cookie
		for _, cookie := range loginResponse.Cookies() {
			if cookie.Name == signalsd.RefreshTokenCookieName {
				originaRefreshTokenCookie = cookie
				break
			}
		}

		if originaRefreshTokenCookie == nil {
			t.Fatal("No refresh token cookie found in login response")
		}

		tests := []struct {
			name           string
			cookie         *http.Cookie // when nil the cookie from the last sucessful login will be used
			expectedStatus int
			expectError    bool
		}{
			{
				name:           "valid refresh_token",
				cookie:         originaRefreshTokenCookie,
				expectedStatus: http.StatusOK,
				expectError:    false,
			},
			{
				// the previous sucessful refresh should have revoked the original refresh token
				name:           "revoked refresh_token",
				cookie:         originaRefreshTokenCookie,
				expectedStatus: http.StatusUnauthorized,
				expectError:    true,
			},
			{
				name:           "valid refresh_token (second refresh)",
				cookie:         nil, // use the latest refresh token from previous successful test
				expectedStatus: http.StatusOK,
				expectError:    false,
			},
			{
				name: "invalid refresh_token",
				cookie: &http.Cookie{
					Name:  signalsd.RefreshTokenCookieName,
					Value: "invalid-token",
					Path:  "/",
				},
				expectedStatus: http.StatusUnauthorized,
				expectError:    true,
			},
		}

		var latestRefreshTokenCookie *http.Cookie
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				if tt.cookie == nil {
					tt.cookie = latestRefreshTokenCookie
				}
				response := makeOAuthTokenRequest(t, baseURL, "refresh_token", nil, tt.cookie.Value)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Errorf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
					return
				}

				var responseBody map[string]any
				if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if tt.expectError {
					if _, hasErrorCode := responseBody["error_code"]; !hasErrorCode {
						t.Error("Expected error_code in error response")
						return
					}
					return
				}

				if _, ok := responseBody["access_token"]; !ok {
					t.Error("Expected access_token in response")
				}
				if tokenType, ok := responseBody["token_type"].(string); !ok || tokenType != "Bearer" {
					t.Errorf("Expected token_type 'Bearer', got %v", responseBody["token_type"])
				}

				expectedExpiry := time.Now()

				// the cookie rotation should have set a new http cookie
				hasNewRefreshCookie := false
				for _, cookie := range response.Cookies() {
					if cookie.Name == signalsd.RefreshTokenCookieName {
						hasNewRefreshCookie = true
						latestRefreshTokenCookie = cookie
						break
					}
				}
				if !hasNewRefreshCookie {
					t.Fatal("Expected new refresh token cookie in response")
				}

				latestExpiry := expectedExpiry.Add(signalsd.RefreshTokenExpiry).Add(testTolerance)
				earliestExpiry := expectedExpiry.Add(signalsd.RefreshTokenExpiry).Add(-testTolerance)

				//check the cookie is correctly configured
				// Handle both MaxAge and Expires approaches
				var cookieExpiry time.Time
				if latestRefreshTokenCookie.MaxAge > 0 {
					// Cookie uses MaxAge - calculate expiry from current time
					cookieExpiry = time.Now().Add(time.Duration(latestRefreshTokenCookie.MaxAge) * time.Second)
				} else if !latestRefreshTokenCookie.Expires.IsZero() {
					// Cookie uses Expires field
					cookieExpiry = latestRefreshTokenCookie.Expires
				} else {
					t.Error("Cookie has neither MaxAge nor Expires set")
					return
				}

				if !latestExpiry.After(cookieExpiry) || earliestExpiry.After(cookieExpiry) {
					t.Errorf("the refresh token should expire between %v and %v but got : %v", earliestExpiry, latestExpiry, cookieExpiry)
				}

				if latestRefreshTokenCookie.Path != "/oauth" {
					t.Errorf("Expected the cookie path to be /oauth but got %v", latestRefreshTokenCookie.Path)
				}
				if !latestRefreshTokenCookie.HttpOnly {
					t.Errorf("httpOnly should be set to true but found :%v ", latestRefreshTokenCookie.HttpOnly)
				}

			})
		}
	})

	t.Run("invalid grant_type", func(t *testing.T) {
		response := makeOAuthTokenRequest(t, baseURL, "invalid_grant", nil, "")
		defer response.Body.Close()

		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", response.StatusCode)
		}

		var responseBody map[string]any
		if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if _, hasErrorCode := responseBody["error_code"]; !hasErrorCode {
			t.Error("Expected error_code in error response")
		}
	})
}

// makeOAuthTokenRequest makes a POST request to /oauth/token
func makeOAuthTokenRequest(t *testing.T, baseURL, grantType string, payload map[string]string, refreshToken string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/oauth/token?grant_type=%s", baseURL, grantType)

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add refresh token cookie if provided
	if refreshToken != "" {
		req.AddCookie(&http.Cookie{
			Name:  signalsd.RefreshTokenCookieName,
			Value: refreshToken,
			Path:  "/",
		})
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	return resp
}

// makeOAuthRevokeRequest makes a POST request to /oauth/revoke
func makeOAuthRevokeRequest(t *testing.T, baseURL string, payload map[string]string, refreshToken string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/oauth/revoke", baseURL)

	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatalf("Failed to marshal payload: %v", err)
		}
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add refresh token cookie if provided
	if refreshToken != "" {
		req.AddCookie(&http.Cookie{
			Name:  signalsd.RefreshTokenCookieName,
			Value: refreshToken,
			Path:  "/",
		})
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	return resp
}

// makeLoginRequest makes a POST request to /api/auth/login
func makeLoginRequest(t *testing.T, baseURL string, payload map[string]string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/auth/login", baseURL)

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	return resp
}

// TestOAuthRevokeEndpoint tests POST /oauth/revoke for both account types
func TestOAuthRevokeEndpoint(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// Start server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL, "")
	defer stopServer()

	t.Log("Creating test data...")

	// Create test user account
	_ = createTestUserWithPassword(t, ctx, testEnv.queries, testEnv.authService, "member", "revoke@oauth.test", "password123")

	// Create test service account
	serviceAccount := createTestAccount(t, ctx, testEnv.queries, "member", "service_account", "revoke-service@oauth.test")

	// Create client secret for service account
	clientSecret, err := testEnv.authService.GenerateSecureToken(32)
	if err != nil {
		t.Fatalf("Failed to generate client secret: %v", err)
	}

	hashedSecret := testEnv.authService.HashToken(clientSecret)
	_, err = testEnv.queries.CreateClientSecret(ctx, database.CreateClientSecretParams{
		ServiceAccountAccountID: serviceAccount.ID,
		HashedSecret:            hashedSecret,
		ExpiresAt:               time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Failed to create client secret: %v", err)
	}

	// Get client_id for service account
	serviceAccountDetails, err := testEnv.queries.GetServiceAccountByAccountID(ctx, serviceAccount.ID)
	if err != nil {
		t.Fatalf("Failed to get service account details: %v", err)
	}

	t.Run("revoke service_account credentials", func(t *testing.T) {
		// First get an access token for the service account
		tokenPayload := map[string]string{
			"client_id":     serviceAccountDetails.ClientID,
			"client_secret": clientSecret,
		}
		tokenResponse := makeOAuthTokenRequest(t, baseURL, "client_credentials", tokenPayload, "")
		defer tokenResponse.Body.Close()

		if tokenResponse.StatusCode != http.StatusOK {
			t.Fatalf("Failed to get access token: %d", tokenResponse.StatusCode)
		}

		var tokenResponseBody map[string]any
		if err := json.NewDecoder(tokenResponse.Body).Decode(&tokenResponseBody); err != nil {
			t.Fatalf("Failed to decode token response: %v", err)
		}

		_, ok := tokenResponseBody["access_token"].(string)
		if !ok {
			t.Fatal("Failed to get access token from token response")
		}

		// Test revoke with valid credentials (service accounts use client credentials, not access token)
		revokeResponse := makeOAuthRevokeRequest(t, baseURL, tokenPayload, "")
		defer revokeResponse.Body.Close()

		if revokeResponse.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", revokeResponse.StatusCode)
		}

		// Verify client secret was revoked by trying to get another token
		retryResponse := makeOAuthTokenRequest(t, baseURL, "client_credentials", tokenPayload, "")
		defer retryResponse.Body.Close()

		if retryResponse.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected revoked credentials to fail with 401, got %d", retryResponse.StatusCode)
		}
	})

	t.Run("revoke user refresh_token", func(t *testing.T) {
		// First login to get tokens
		loginPayload := map[string]string{
			"email":    "revoke@oauth.test",
			"password": "password123",
		}
		loginResponse := makeLoginRequest(t, baseURL, loginPayload)
		defer loginResponse.Body.Close()

		if loginResponse.StatusCode != http.StatusOK {
			t.Fatalf("Login failed: %d", loginResponse.StatusCode)
		}

		var loginResponseBody map[string]any
		if err := json.NewDecoder(loginResponse.Body).Decode(&loginResponseBody); err != nil {
			t.Fatalf("Failed to decode login response: %v", err)
		}

		// Extract refresh token cookie
		var refreshTokenCookie *http.Cookie
		for _, cookie := range loginResponse.Cookies() {
			if cookie.Name == signalsd.RefreshTokenCookieName {
				refreshTokenCookie = cookie
				break
			}
		}

		if refreshTokenCookie == nil {
			t.Fatal("No refresh token cookie found in login response")
		}

		// Test revoke with valid refresh token
		revokeResponse := makeOAuthRevokeRequest(t, baseURL, nil, refreshTokenCookie.Value)
		defer revokeResponse.Body.Close()

		if revokeResponse.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", revokeResponse.StatusCode)
		}

		// Verify refresh token was revoked by trying to use it
		retryResponse := makeOAuthTokenRequest(t, baseURL, "refresh_token", nil, refreshTokenCookie.Value)
		defer retryResponse.Body.Close()

		if retryResponse.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected revoked refresh token to fail with 401, got %d", retryResponse.StatusCode)
		}
	})
}
