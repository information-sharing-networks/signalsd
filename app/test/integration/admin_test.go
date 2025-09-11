//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
)

type registrationDetails struct {
	Organization string `json:"client_organization"`
	Email        string `json:"client_contact_email"`
}

// getAccessToken creates an access token for testing
func getAccessToken(t *testing.T, authService *auth.AuthService, accountID uuid.UUID) string {
	ctx := auth.ContextWithAccountID(context.Background(), accountID)
	tokenResponse, err := authService.CreateAccessToken(ctx)
	if err != nil {
		t.Fatalf("Failed to create access token: %v", err)
	}
	return tokenResponse.AccessToken
}

// makeServiceAccountRegRequest makes a POST request to the service account registration endpoint
func makeServiceAccountRegRequest(t *testing.T, baseURL, token string, requestBody registrationDetails) *http.Response {
	requestURL := fmt.Sprintf("%s/api/auth/service-accounts/register", baseURL)

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	return response
}

// TestServiceAccountRegistration tests the POST /api/auth/service-accounts/register endpoint
func TestServiceAccountRegistration(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// Start server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()

	t.Log("Creating test data...")

	// Create test accounts with different roles
	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@test.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "admin", "user", "admin@test.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@test.com")

	// Get access tokens
	ownerToken := getAccessToken(t, testEnv.authService, ownerAccount.ID)
	adminToken := getAccessToken(t, testEnv.authService, adminAccount.ID)
	memberToken := getAccessToken(t, testEnv.authService, memberAccount.ID)

	t.Run("permissions tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    registrationDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "owner_can_register",
				requestBody:    registrationDetails{"Owner Organization", "owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusCreated,
			},
			{
				name:           "admin_can_register",
				requestBody:    registrationDetails{"Admin Organization", "admin@example.com"},
				token:          adminToken,
				expectedStatus: http.StatusCreated,
			},
			{
				name:           "member_cannot_register",
				requestBody:    registrationDetails{"Member Organization", "member@example.com"},
				token:          memberToken,
				expectedStatus: http.StatusForbidden,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeServiceAccountRegRequest(t, baseURL, tt.token, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
					return
				}

				if response.StatusCode == http.StatusForbidden {
					return
				}

				var result map[string]any
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				// Verify response structure
				if _, exists := result["client_id"]; !exists {
					t.Error("Response missing 'client_id' field")
				}
				if _, exists := result["setup_url"]; !exists {
					t.Error("Response missing 'setup_url' field")
				}

				// Verify the service account was created in the database
				clientID := result["client_id"].(string)
				serviceAccount, err := testEnv.queries.GetServiceAccountByClientID(ctx, clientID)
				if err != nil {
					t.Fatalf("Failed to find created service account: %v", err)
				}

				if serviceAccount.ClientContactEmail != tt.requestBody.Email {
					t.Errorf("Expected email %s, got %s", tt.requestBody.Email, serviceAccount.ClientContactEmail)
				}
				if serviceAccount.ClientOrganization != tt.requestBody.Organization {
					t.Errorf("Expected organization %s, got %s", tt.requestBody.Organization, serviceAccount.ClientOrganization)
				}
			})
		}
	})

	t.Run("duplicate_registration_conflict", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    registrationDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "cannot_register_duplicate_email/org",
				requestBody:    registrationDetails{"Owner Organization", "owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusConflict,
			},
			{
				name:           "cannot_register_duplicate_email/email_mixed_case",
				requestBody:    registrationDetails{"Owner Organization", "Owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusConflict,
			},
			{
				name:           "cannot_register_duplicate_email/org_case_insensitive",
				requestBody:    registrationDetails{"OWNER ORGANIZATION", "owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusConflict,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeServiceAccountRegRequest(t, baseURL, tt.token, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
					return
				}
			})
		}
	})
	t.Run("validation tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    registrationDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "missing organization",
				requestBody:    registrationDetails{Email: "owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "missing email",
				requestBody:    registrationDetails{Organization: "Owner Organization"},
				token:          ownerToken,
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "empty organization",
				requestBody:    registrationDetails{Email: "owner@example.com", Organization: ""},
				token:          ownerToken,
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "empty email",
				requestBody:    registrationDetails{Email: "", Organization: "Owner Organization"},
				token:          ownerToken,
				expectedStatus: http.StatusBadRequest,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeServiceAccountRegRequest(t, baseURL, tt.token, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}
				if tt.expectedStatus >= 400 {
					var errorResponse map[string]any
					if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
						t.Logf("Failed to decode error response (this might be expected for some error types): %v", err)
					} else {
						if _, exists := errorResponse["error_code"]; !exists {
							t.Error("Error response missing 'error_code' field")
						}
						if _, exists := errorResponse["message"]; !exists {
							t.Error("Error response missing 'message' field")
						}
					}
				}
			})
		}
	})
}

func TestServiceAccountReissue(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// Start server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()

	t.Log("Creating test data...")

	// Create test accounts with different roles
	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@test.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "admin", "user", "admin@test.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@test.com")

	// Get access tokens
	ownerToken := getAccessToken(t, testEnv.authService, ownerAccount.ID)
	adminToken := getAccessToken(t, testEnv.authService, adminAccount.ID)
	memberToken := getAccessToken(t, testEnv.authService, memberAccount.ID)

	adminServiceAccountDetails := registrationDetails{"Admin Organization", "admin@example.com"}

	response := makeServiceAccountRegRequest(t, baseURL, adminToken, adminServiceAccountDetails)
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("could not create test service account: %d", response.StatusCode)
	}

	t.Run("permissions tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			token          string
			expectedStatus int
		}{
			{
				name:           "owner_can_reissue",
				token:          ownerToken,
				expectedStatus: http.StatusOK,
			},
			{
				name:           "admin_can_reissue",
				token:          adminToken,
				expectedStatus: http.StatusOK,
			},
			{
				name:           "member_cannot_reissue",
				token:          memberToken,
				expectedStatus: http.StatusForbidden,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeServiceAccountReissueRequest(t, baseURL, tt.token, adminServiceAccountDetails)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
					return
				}

				if response.StatusCode == http.StatusForbidden {
					return
				}

				var result map[string]any
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				// Verify response structure
				if _, exists := result["client_id"]; !exists {
					t.Error("Response missing 'client_id' field")
				}
				if _, exists := result["setup_url"]; !exists {
					t.Error("Response missing 'setup_url' field")
				}

			})
		}
	})

	t.Run("case insenstive tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    registrationDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "can_reissue_case_insensitive_email",
				token:          ownerToken,
				requestBody:    registrationDetails{"Admin Organization", "Admin@example.com"},
				expectedStatus: http.StatusOK,
			},
			{
				name:           "can_reissue_case_insensitive_org",
				token:          ownerToken,
				requestBody:    registrationDetails{"ADMIN ORGANIZATION", "admin@example.com"},
				expectedStatus: http.StatusOK,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeServiceAccountReissueRequest(t, baseURL, tt.token, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
					return
				}

				var result map[string]any
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				// Verify response structure
				if _, exists := result["client_id"]; !exists {
					t.Error("Response missing 'client_id' field")
				}
				if _, exists := result["setup_url"]; !exists {
					t.Error("Response missing 'setup_url' field")
				}

			})
		}
	})
}

// makeServiceAccountReissueRequest makes a POST request to the service account credential reissuing endpoint
func makeServiceAccountReissueRequest(t *testing.T, baseURL, token string, requestBody registrationDetails) *http.Response {
	requestURL := fmt.Sprintf("%s/api/auth/service-accounts/reissue-credentials", baseURL)

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	return response
}
