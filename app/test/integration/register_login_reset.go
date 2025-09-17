//go:build integration

package integration

//e2e tests for
// Register new service account
// Service account reissue credentials
// User registration
// User login
// User password reset (admin generated link)
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

type serviceAccountDetails struct {
	Organization string `json:"client_organization"`
	Email        string `json:"client_contact_email"`
}

type loginDetails struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userDetails struct {
	Email    string `json:"email"`
	Password string `json:"password"`
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
func makeServiceAccountRegRequest(t *testing.T, baseURL, token string, requestBody serviceAccountDetails) *http.Response {
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

// makeUserLoginRequest makes a POST request to the login endpoint
func makeUserLoginRequest(t *testing.T, baseURL string, requestBody loginDetails) *http.Response {
	requestURL := fmt.Sprintf("%s/api/auth/login", baseURL)

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	return response
}

// makeUserRegistrationRequest makes a POST request to the user registration endpoint
func makeUserRegistrationRequest(t *testing.T, baseURL string, requestBody userDetails) *http.Response {
	requestURL := fmt.Sprintf("%s/api/auth/register", baseURL)

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

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
			requestBody    serviceAccountDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "owner_can_register",
				requestBody:    serviceAccountDetails{"Owner Organization", "owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusCreated,
			},
			{
				name:           "admin_can_register",
				requestBody:    serviceAccountDetails{"Admin Organization", "admin@example.com"},
				token:          adminToken,
				expectedStatus: http.StatusCreated,
			},
			{
				name:           "member_cannot_register",
				requestBody:    serviceAccountDetails{"Member Organization", "member@example.com"},
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
			requestBody    serviceAccountDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "cannot_register_duplicate_email/org",
				requestBody:    serviceAccountDetails{"Owner Organization", "owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusConflict,
			},
			{
				name:           "cannot_register_duplicate_email/email_mixed_case",
				requestBody:    serviceAccountDetails{"Owner Organization", "Owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusConflict,
			},
			{
				name:           "cannot_register_duplicate_email/org_case_insensitive",
				requestBody:    serviceAccountDetails{"OWNER ORGANIZATION", "owner@example.com"},
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
			requestBody    serviceAccountDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "missing organization",
				requestBody:    serviceAccountDetails{Email: "owner@example.com"},
				token:          ownerToken,
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "missing email",
				requestBody:    serviceAccountDetails{Organization: "Owner Organization"},
				token:          ownerToken,
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "empty organization",
				requestBody:    serviceAccountDetails{Email: "owner@example.com", Organization: ""},
				token:          ownerToken,
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "empty email",
				requestBody:    serviceAccountDetails{Email: "", Organization: "Owner Organization"},
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

	adminServiceAccountDetails := serviceAccountDetails{"Admin Organization", "admin@example.com"}

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
			requestBody    serviceAccountDetails
			token          string
			expectedStatus int
		}{
			{
				name:           "can_reissue_case_insensitive_email",
				token:          ownerToken,
				requestBody:    serviceAccountDetails{"Admin Organization", "Admin@example.com"},
				expectedStatus: http.StatusOK,
			},
			{
				name:           "can_reissue_case_insensitive_org",
				token:          ownerToken,
				requestBody:    serviceAccountDetails{"ADMIN ORGANIZATION", "admin@example.com"},
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
func makeServiceAccountReissueRequest(t *testing.T, baseURL, token string, requestBody serviceAccountDetails) *http.Response {
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

// TestUserLogin tests the POST /api/auth/login endpoint
func TestUserLogin(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// Start server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()

	t.Log("Creating test data...")

	// Create test users with passwords
	ownerPassword := "ownerpassword123"
	adminPassword := "adminpassword123"
	memberPassword := "memberpassword123"

	ownerAccount := createTestUserWithPassword(t, ctx, testEnv.queries, testEnv.authService, "owner", "owner@login.test", ownerPassword)
	adminAccount := createTestUserWithPassword(t, ctx, testEnv.queries, testEnv.authService, "admin", "admin@login.test", adminPassword)
	memberAccount := createTestUserWithPassword(t, ctx, testEnv.queries, testEnv.authService, "member", "member@login.test", memberPassword)

	t.Run("successful login tests", func(t *testing.T) {
		testCases := []struct {
			name            string
			requestBody     loginDetails
			expectedStatus  int
			expectedRole    string
			expectedAccount uuid.UUID
		}{
			{
				name:            "owner_can_login",
				requestBody:     loginDetails{"owner@login.test", ownerPassword},
				expectedStatus:  http.StatusOK,
				expectedRole:    "owner",
				expectedAccount: ownerAccount.ID,
			},
			{
				name:            "admin_can_login",
				requestBody:     loginDetails{"admin@login.test", adminPassword},
				expectedStatus:  http.StatusOK,
				expectedRole:    "admin",
				expectedAccount: adminAccount.ID,
			},
			{
				name:            "member_can_login",
				requestBody:     loginDetails{"member@login.test", memberPassword},
				expectedStatus:  http.StatusOK,
				expectedRole:    "member",
				expectedAccount: memberAccount.ID,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeUserLoginRequest(t, baseURL, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}

				var result map[string]any
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				// Verify response structure
				if _, exists := result["access_token"]; !exists {
					t.Error("Response missing 'access_token' field")
				}
				if _, exists := result["token_type"]; !exists {
					t.Error("Response missing 'token_type' field")
				}
				if _, exists := result["expires_in"]; !exists {
					t.Error("Response missing 'expires_in' field")
				}
				if _, exists := result["account_id"]; !exists {
					t.Error("Response missing 'account_id' field")
				}
				if _, exists := result["account_type"]; !exists {
					t.Error("Response missing 'account_type' field")
				}
				if _, exists := result["role"]; !exists {
					t.Error("Response missing 'role' field")
				}

				// Verify role matches expected
				if role, ok := result["role"].(string); ok {
					if role != tt.expectedRole {
						t.Errorf("Expected role %s, got %s", tt.expectedRole, role)
					}
				} else {
					t.Error("Role field is not a string")
				}

				// Verify account_id matches expected
				if accountID, ok := result["account_id"].(string); ok {
					if accountID != tt.expectedAccount.String() {
						t.Errorf("Expected account_id %s, got %s", tt.expectedAccount.String(), accountID)
					}
				} else {
					t.Error("Account_id field is not a string")
				}

				// Verify account_type is "user"
				if accountType, ok := result["account_type"].(string); ok {
					if accountType != "user" {
						t.Errorf("Expected account_type 'user', got %s", accountType)
					}
				} else {
					t.Error("Account_type field is not a string")
				}

				// Verify token_type is "Bearer"
				if tokenType, ok := result["token_type"].(string); ok {
					if tokenType != "Bearer" {
						t.Errorf("Expected token_type 'Bearer', got %s", tokenType)
					}
				} else {
					t.Error("Token_type field is not a string")
				}

				// Check that refresh token cookie is set
				var refreshTokenCookie *http.Cookie
				for _, cookie := range response.Cookies() {
					if cookie.Name == "refresh_token" {
						refreshTokenCookie = cookie
						break
					}
				}
				if refreshTokenCookie == nil {
					t.Error("Expected refresh_token cookie to be set")
				} else {
					if refreshTokenCookie.HttpOnly != true {
						t.Error("Expected refresh_token cookie to be HttpOnly")
					}
					if refreshTokenCookie.Value == "" {
						t.Error("Expected refresh_token cookie to have a value")
					}
				}
			})
		}
	})

	t.Run("authentication failure tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    loginDetails
			expectedStatus int
		}{
			{
				name:           "wrong_password",
				requestBody:    loginDetails{"owner@login.test", "wrongpassword"},
				expectedStatus: http.StatusUnauthorized,
			},
			{
				name:           "nonexistent_email",
				requestBody:    loginDetails{"nonexistent@login.test", "anypassword"},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "case_insensitive_email_match",
				requestBody:    loginDetails{"Owner@login.test", ownerPassword}, // Should work - emails are case insensitive
				expectedStatus: http.StatusOK,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeUserLoginRequest(t, baseURL, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}

				if tt.expectedStatus >= 400 {
					var errorResponse map[string]any
					if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
						t.Fatalf("Failed to decode error response: %v", err)
					}

					// Verify error response structure
					if _, exists := errorResponse["error_code"]; !exists {
						t.Error("Error response missing 'error_code' field")
					}
					if _, exists := errorResponse["message"]; !exists {
						t.Error("Error response missing 'message' field")
					}
				}
			})
		}
	})

	t.Run("validation tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    loginDetails
			expectedStatus int
		}{
			{
				name:           "missing_email",
				requestBody:    loginDetails{Password: ownerPassword},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "missing_password",
				requestBody:    loginDetails{Email: "owner@login.test"},
				expectedStatus: http.StatusUnauthorized,
			},
			{
				name:           "empty_email",
				requestBody:    loginDetails{Email: "", Password: ownerPassword},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:           "empty_password",
				requestBody:    loginDetails{Email: "owner@login.test", Password: ""},
				expectedStatus: http.StatusUnauthorized,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeUserLoginRequest(t, baseURL, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}

				if tt.expectedStatus >= 400 {
					var errorResponse map[string]any
					if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
						t.Logf("Failed to decode error response %v", err)
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

	t.Run("disabled account tests", func(t *testing.T) {
		// Create a test user and then disable them
		disabledPassword := "disabledpassword123"
		disabledAccount := createTestUserWithPassword(t, ctx, testEnv.queries, testEnv.authService, "member", "disabled@login.test", disabledPassword)

		// Disable the account
		_, err := testEnv.queries.DisableAccount(ctx, disabledAccount.ID)
		if err != nil {
			t.Fatalf("Failed to disable account: %v", err)
		}

		t.Run("disabled_account_cannot_login", func(t *testing.T) {
			response := makeUserLoginRequest(t, baseURL, loginDetails{"disabled@login.test", disabledPassword})
			defer response.Body.Close()

			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("Expected status %d, got %d", http.StatusUnauthorized, response.StatusCode)
			}

			var errorResponse map[string]any
			if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			// Verify error response structure
			if _, exists := errorResponse["error_code"]; !exists {
				t.Error("Error response missing 'error_code' field")
			}
			if _, exists := errorResponse["message"]; !exists {
				t.Error("Error response missing 'message' field")
			}
		})
	})
}

// TestUserRegistration tests the POST /api/auth/register endpoint
func TestUserRegistration(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// Start server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()

	t.Log("Testing user registration endpoint...")

	t.Run("successful registration tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    userDetails
			expectedStatus int
			expectedRole   string
			isFirstUser    bool
		}{
			{
				name:           "first_user_becomes_owner",
				requestBody:    userDetails{"owner@register.test", "validpassword123"},
				expectedStatus: http.StatusCreated,
				expectedRole:   "owner",
				isFirstUser:    true,
			},
			{
				name:           "second_user_becomes_member",
				requestBody:    userDetails{"member@register.test", "validpassword123"},
				expectedStatus: http.StatusCreated,
				expectedRole:   "member",
				isFirstUser:    false,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeUserRegistrationRequest(t, baseURL, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}

				// Verify the user was created in the database with correct role
				user, err := testEnv.queries.GetUserByEmail(ctx, tt.requestBody.Email)
				if err != nil {
					t.Fatalf("Failed to find created user: %v", err)
				}

				if user.Email != strings.ToLower(tt.requestBody.Email) {
					t.Errorf("Expected email %s, got %s", strings.ToLower(tt.requestBody.Email), user.Email)
				}

				if user.UserRole != tt.expectedRole {
					t.Errorf("Expected role %s, got %s", tt.expectedRole, user.UserRole)
				}

				// Verify password was hashed correctly by attempting to check it
				err = testEnv.authService.CheckPasswordHash(user.HashedPassword, tt.requestBody.Password)
				if err != nil {
					t.Errorf("Password hash verification failed: %v", err)
				}

				// Verify account was created and is active
				account, err := testEnv.queries.GetAccountByID(ctx, user.AccountID)
				if err != nil {
					t.Fatalf("Failed to find created account: %v", err)
				}

				if account.AccountType != "user" {
					t.Errorf("Expected account type 'user', got %s", account.AccountType)
				}

				if !account.IsActive {
					t.Error("Expected account to be active")
				}

				if account.AccountRole != tt.expectedRole {
					t.Errorf("Expected account role %s, got %s", tt.expectedRole, account.AccountRole)
				}
			})
		}
	})

	t.Run("duplicate email tests", func(t *testing.T) {
		// First, register a user
		firstUser := userDetails{"duplicate@example.com", "validpassword123"}
		response := makeUserRegistrationRequest(t, baseURL, firstUser)
		response.Body.Close()

		if response.StatusCode != http.StatusCreated {
			t.Fatalf("Failed to create first user: status %d", response.StatusCode)
		}

		testCases := []struct {
			name           string
			requestBody    userDetails
			expectedStatus int
		}{
			{
				name:           "cannot_register_duplicate_email",
				requestBody:    userDetails{"duplicate@example.com", "anotherpassword123"},
				expectedStatus: http.StatusConflict,
			},
			{
				name:           "cannot_register_duplicate_email_case_insensitive",
				requestBody:    userDetails{"DUPLICATE@example.com", "anotherpassword123"},
				expectedStatus: http.StatusConflict,
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeUserRegistrationRequest(t, baseURL, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}

				if tt.expectedStatus >= 400 {
					var errorResponse map[string]any
					if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
						t.Fatalf("Failed to decode error response: %v", err)
					}

					// Verify error response structure
					if _, exists := errorResponse["error_code"]; !exists {
						t.Error("Error response missing 'error_code' field")
					}
					if _, exists := errorResponse["message"]; !exists {
						t.Error("Error response missing 'message' field")
					}

					// Verify specific error code for duplicate email
					if errorCode, ok := errorResponse["error_code"].(string); ok {
						if errorCode != "resource_already_exists" {
							t.Errorf("Expected error_code 'resource_already_exists', got %s", errorCode)
						}
					}
				}
			})
		}
	})

	t.Run("validation tests", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestBody    userDetails
			expectedStatus int
			expectedError  string
		}{
			{
				name:           "missing_email",
				requestBody:    userDetails{"", "validpassword123"},
				expectedStatus: http.StatusBadRequest,
				expectedError:  "malformed_body",
			},
			{
				name:           "missing_password",
				requestBody:    userDetails{"test@register.test", ""},
				expectedStatus: http.StatusBadRequest,
				expectedError:  "malformed_body",
			},
			{
				name:           "password_too_short",
				requestBody:    userDetails{"test@register.test", "short"},
				expectedStatus: http.StatusBadRequest,
				expectedError:  "password_too_short",
			},
			{
				name:           "password_exactly_minimum_length",
				requestBody:    userDetails{"minlength@register.test", "password123"}, // 11 chars
				expectedStatus: http.StatusCreated,
				expectedError:  "",
			},
		}
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				response := makeUserRegistrationRequest(t, baseURL, tt.requestBody)
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}

				if tt.expectedStatus >= 400 {
					var errorResponse map[string]any
					if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
						t.Fatalf("Failed to decode error response: %v", err)
					}

					// Verify error response structure
					if _, exists := errorResponse["error_code"]; !exists {
						t.Error("Error response missing 'error_code' field")
					}
					if _, exists := errorResponse["message"]; !exists {
						t.Error("Error response missing 'message' field")
					}

					// Verify specific error code
					if tt.expectedError != "" {
						if errorCode, ok := errorResponse["error_code"].(string); ok {
							if errorCode != tt.expectedError {
								t.Errorf("Expected error_code '%s', got %s", tt.expectedError, errorCode)
							}
						}
					}
				} else if tt.expectedStatus == http.StatusCreated {
					// For successful registration, verify user was created
					user, err := testEnv.queries.GetUserByEmail(ctx, tt.requestBody.Email)
					if err != nil {
						t.Fatalf("Failed to find created user: %v", err)
					}

					if user.Email != strings.ToLower(tt.requestBody.Email) {
						t.Errorf("Expected email %s, got %s", strings.ToLower(tt.requestBody.Email), user.Email)
					}
				}
			})
		}
	})

	t.Run("malformed request tests", func(t *testing.T) {
		t.Run("invalid_json", func(t *testing.T) {
			requestURL := fmt.Sprintf("%s/api/auth/register", baseURL)

			// Send malformed JSON
			req, err := http.NewRequest("POST", requestURL, strings.NewReader("{invalid json"))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 10 * time.Second}
			response, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, response.StatusCode)
			}

			var errorResponse map[string]any
			if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			// Verify error response structure
			if _, exists := errorResponse["error_code"]; !exists {
				t.Error("Error response missing 'error_code' field")
			}
			if _, exists := errorResponse["message"]; !exists {
				t.Error("Error response missing 'message' field")
			}

			// Verify specific error code for malformed JSON
			if errorCode, ok := errorResponse["error_code"].(string); ok {
				if errorCode != "malformed_body" {
					t.Errorf("Expected error_code 'malformed_body', got %s", errorCode)
				}
			}
		})
	})
}

func TestPasswordResetFlow(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	queries := database.New(testDB)
	authService := auth.NewAuthService(secretKey, "test", queries)

	// Create test accounts
	adminAccount := createTestAccount(t, ctx, queries, "admin", "user", "admin@example.com")
	userAccount := createTestAccount(t, ctx, queries, "member", "user", "user@example.com")

	// Start test server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testDB, testURL)
	defer stopServer()

	t.Run("non admin cannot generate reset link", func(t *testing.T) {
		userToken := getAccessToken(t, authService, userAccount.ID)

		url := fmt.Sprintf("%s/api/admin/users/%s/generate-password-reset-link", baseURL, userAccount.ID)
		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+userToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("Expected status 403, got %d", resp.StatusCode)
		}
	})

	// 1. Admin generates password reset link
	t.Run("Admin generates password reset link", func(t *testing.T) {
		// Get admin access token
		adminToken := getAccessToken(t, authService, adminAccount.ID)

		// Generate password reset link
		url := fmt.Sprintf("%s/api/admin/users/%s/generate-password-reset-link", baseURL, userAccount.ID)
		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var resetResponse handlers.GeneratePasswordResetLinkResponse
		if err := json.NewDecoder(resp.Body).Decode(&resetResponse); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Verify response structure
		if resetResponse.UserEmail != "user@example.com" {
			t.Errorf("Expected user email 'user@example.com', got '%s'", resetResponse.UserEmail)
		}
		if resetResponse.AccountID != userAccount.ID {
			t.Errorf("Expected account ID %s, got %s", userAccount.ID, resetResponse.AccountID)
		}
		if !strings.Contains(resetResponse.ResetURL, "/api/auth/password-reset/") {
			t.Errorf("Expected reset URL to contain '/api/auth/password-reset/', got '%s'", resetResponse.ResetURL)
		}
		if resetResponse.ExpiresIn != int(signalsd.PasswordResetExpiry.Seconds()) {
			t.Errorf("Expected expires_in %d, got %d", int(signalsd.PasswordResetExpiry.Seconds()), resetResponse.ExpiresIn)
		}

		// Extract token ID from URL for next steps
		urlParts := strings.Split(resetResponse.ResetURL, "/")
		tokenID := urlParts[len(urlParts)-1]

		// 2: User visits reset link (GET) - should render HTML form
		t.Run("User visits reset link", func(t *testing.T) {
			resetURL := fmt.Sprintf("%s/api/auth/password-reset/%s", baseURL, tokenID)
			resp, err := http.Get(resetURL)
			if err != nil {
				t.Fatalf("Failed to get reset form: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d", resp.StatusCode)
			}

			// Verify content type is HTML
			contentType := resp.Header.Get("Content-Type")
			if !strings.Contains(contentType, "text/html") {
				t.Errorf("Expected HTML content type, got '%s'", contentType)
			}

			// Read response body to verify it contains form elements
			buf := new(bytes.Buffer)
			buf.ReadFrom(resp.Body)
			body := buf.String()

			// Verify HTML form is present
			if !strings.Contains(body, "Reset Your Password") {
				t.Error("Expected HTML to contain 'Reset Your Password' title")
			}
			if !strings.Contains(body, "user@example.com") {
				t.Error("Expected HTML to contain user email")
			}
			if !strings.Contains(body, `type="password"`) {
				t.Error("Expected HTML to contain password input field")
			}
		})

		// 3: User submits new password
		t.Run("User submits new password", func(t *testing.T) {
			newPassword := "new-secure-password-123"
			resetRequest := handlers.PasswordResetRequest{
				NewPassword: newPassword,
			}

			requestBody, err := json.Marshal(resetRequest)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			resetURL := fmt.Sprintf("%s/api/auth/password-reset/%s", baseURL, tokenID)
			req, err := http.NewRequest("POST", resetURL, bytes.NewBuffer(requestBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Failed to submit password reset: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d", resp.StatusCode)
			}

			// 4: Verify user can login with new password
			t.Run("User can login with new password", func(t *testing.T) {
				loginRequest := map[string]string{
					"email":    "user@example.com",
					"password": newPassword,
				}

				requestBody, err := json.Marshal(loginRequest)
				if err != nil {
					t.Fatalf("Failed to marshal login request: %v", err)
				}

				loginURL := fmt.Sprintf("%s/api/auth/login", baseURL)
				req, err := http.NewRequest("POST", loginURL, bytes.NewBuffer(requestBody))
				if err != nil {
					t.Fatalf("Failed to create login request: %v", err)
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("Failed to login: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("Expected login success with status 200, got %d", resp.StatusCode)
				}

				var loginResponse auth.AccessTokenResponse
				if err := json.NewDecoder(resp.Body).Decode(&loginResponse); err != nil {
					t.Fatalf("Failed to decode login response: %v", err)
				}

				// Verify we got a valid access token
				if loginResponse.AccessToken == "" {
					t.Error("Expected access token in login response")
				}
			})

			// Step 5: Verify reset token cannot be reused
			t.Run("Reset token cannot be reused", func(t *testing.T) {
				anotherResetRequest := handlers.PasswordResetRequest{
					NewPassword: "another-password-456",
				}

				requestBody, err := json.Marshal(anotherResetRequest)
				if err != nil {
					t.Fatalf("Failed to marshal request: %v", err)
				}

				resetURL := fmt.Sprintf("%s/api/auth/password-reset/%s", baseURL, tokenID)
				req, err := http.NewRequest("POST", resetURL, bytes.NewBuffer(requestBody))
				if err != nil {
					t.Fatalf("Failed to create request: %v", err)
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("Failed to attempt password reset: %v", err)
				}
				defer resp.Body.Close()

				// Should return 404 since token was consumed
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("Expected status 404 for consumed token, got %d", resp.StatusCode)
				}
			})
		})
	})
}
