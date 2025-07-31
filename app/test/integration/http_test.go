//go:build integration

// this end-2-end test starts the signalsd http server and runs tests against it. By default the server logs are not included in the test output, you can enable them with:
//
//	ENABLE_SERVER_LOGS=true go test -tags=integration -v ./test/integration
//
// Each test creates an empty temporary database and applies all the migrations so the schema reflects the latest code. The database is dropped after each test.
//
// the goal of these tests is to ensure that signals are correctly loaded and can only be seen by authorized users.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testEnvironment struct {
	dbConn      *pgxpool.Pool
	queries     *database.Queries
	authService *auth.AuthService
}

func setupTestEnvironment(dbConn *pgxpool.Pool) *testEnvironment {
	env := &testEnvironment{
		dbConn:      dbConn,
		queries:     database.New(dbConn),
		authService: auth.NewAuthService(secretKey, environment, database.New(dbConn)),
	}
	return env
}

func (env *testEnvironment) createAuthToken(t *testing.T, accountID uuid.UUID) string {
	ctx := auth.ContextWithAccountID(context.Background(), accountID)
	tokenResponse, err := env.authService.CreateAccessToken(ctx)
	if err != nil {
		t.Fatalf("Failed to create access token: %v", err)
	}
	return tokenResponse.AccessToken
}

type testSignalEndpoint struct {
	isnSlug          string
	signalTypeSlug   string
	signalTypeSemVer string
}

// createValidSignalPayload creates json valid for https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/integration-test-schema.json
func createValidSignalPayload(localRef string) map[string]any {
	return map[string]any{
		"signals": []map[string]any{
			{
				"local_ref": localRef,
				"content": map[string]any{
					"test": "valid content for simple schema",
				},
			},
		},
	}
}

func createInvalidSignalPayload(localRef string) map[string]any {
	return map[string]any{
		"signals": []map[string]any{
			{
				"local_ref": localRef,
				"content": map[string]any{
					"invalid_field": "this should fail schema validation",
					// Missing required "test" field
				},
			},
		},
	}
}

// createMultipleSignalsPayload creates a payload with multiple signals
func createMultipleSignalsPayload(localRefs []string) map[string]any {
	signals := make([]map[string]any, len(localRefs))
	for i, ref := range localRefs {
		signals[i] = map[string]any{
			"local_ref": ref,
			"content": map[string]any{
				"test": fmt.Sprintf("signal content %d", i+1),
			},
		}
	}
	return map[string]any{
		"signals": signals,
	}
}

// createEmptySignalsPayload creates a payload with empty signals array
func createEmptySignalsPayload() map[string]any {
	return map[string]any{
		"signals": []map[string]any{},
	}
}

// logResponseDetails logs detailed response information for debugging failed tests
func logResponseDetails(t *testing.T, response *http.Response, testName string) {
	t.Helper()

	t.Logf("=== Response Details for %s ===", testName)
	t.Logf("Status: %d %s", response.StatusCode, response.Status)
	t.Logf("Headers: %v", response.Header)

	// Read response body for logging (this consumes the body)
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.Logf("Failed to read response body: %v", err)
	} else {
		t.Logf("Body: %s", string(bodyBytes))
	}

	t.Logf("=== End Response Details ===")
}

// TestSignalSubmission tests the signal submission pipeline end-to-end (starts a http server as a go routine)
func TestSignalSubmission(t *testing.T) {

	ctx := context.Background()

	testDB := setupTestDatabase(t, ctx)

	testEnv := setupTestEnvironment(testDB)

	// Get the appropriate database URL for the current environment
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()
	t.Logf("✅ Server started at %s", baseURL)

	// create test data
	t.Log("Creating test data...")

	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@gmail.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "admin", "user", "admin@gmail.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@gmail.com")

	adminISN := createTestISN(t, ctx, testEnv.queries, "admin-isn", "Admin ISN", adminAccount.ID, "private")

	adminSignalType := createTestSignalType(t, ctx, testEnv.queries, adminISN.ID, "admin ISN signal", "1.0.0")

	grantPermission(t, ctx, testEnv.queries, adminISN.ID, memberAccount.ID, "read")

	// todo mixed success / failure batch
	tests := []struct {
		name            string
		accountID       uuid.UUID
		endpoint        testSignalEndpoint
		payloadFunc     func() map[string]any
		expectedStatus  int
		expectError     bool
		skipAuthToken   bool
		customAuthToken string
		expectedStored  int // Expected stored_count in response
		expectedFailed  int // Expected failed_count in response
	}{
		{
			name:      "successful_signal_submission_by_admin",
			accountID: adminAccount.ID,
			endpoint: testSignalEndpoint{
				isnSlug:          adminISN.Slug,
				signalTypeSlug:   adminSignalType.Slug,
				signalTypeSemVer: adminSignalType.SemVer,
			},
			payloadFunc:    func() map[string]any { return createValidSignalPayload("admin-test-001") },
			expectedStatus: http.StatusOK,
			expectError:    false,
			expectedStored: 1,
			expectedFailed: 0,
		},
		{
			name:      "successful_signal_submission_by_owner",
			accountID: ownerAccount.ID,
			endpoint: testSignalEndpoint{
				isnSlug:          adminISN.Slug,
				signalTypeSlug:   adminSignalType.Slug,
				signalTypeSemVer: adminSignalType.SemVer,
			},
			payloadFunc:    func() map[string]any { return createValidSignalPayload("owner-test-001") },
			expectedStatus: http.StatusOK,
			expectError:    false,
			expectedStored: 1,
			expectedFailed: 0,
		},
		{
			name:      "unauthorized_signal_submission_by_member",
			accountID: memberAccount.ID,
			endpoint: testSignalEndpoint{
				isnSlug:          adminISN.Slug,
				signalTypeSlug:   adminSignalType.Slug,
				signalTypeSemVer: adminSignalType.SemVer,
			},
			payloadFunc:    func() map[string]any { return createValidSignalPayload("member-test-001") },
			expectedStatus: http.StatusForbidden,
			expectError:    true,
			expectedStored: 0, // No response body expected for auth failures
			expectedFailed: 0,
		},
		{
			name:      "invalid_auth_token",
			accountID: adminAccount.ID,
			endpoint: testSignalEndpoint{
				isnSlug:          adminISN.Slug,
				signalTypeSlug:   adminSignalType.Slug,
				signalTypeSemVer: adminSignalType.SemVer,
			},
			payloadFunc:     func() map[string]any { return createValidSignalPayload("invalid-auth-001") },
			expectedStatus:  http.StatusUnauthorized,
			expectError:     true,
			customAuthToken: "invalid-token",
			expectedStored:  0, // No response body expected for auth failures
			expectedFailed:  0,
		},
		{
			name:      "schema_validation_failure",
			accountID: adminAccount.ID,
			endpoint: testSignalEndpoint{
				isnSlug:          adminISN.Slug,
				signalTypeSlug:   adminSignalType.Slug,
				signalTypeSemVer: adminSignalType.SemVer,
			},
			payloadFunc:    func() map[string]any { return createInvalidSignalPayload("invalid-schema-001") },
			expectedStatus: http.StatusUnprocessableEntity,
			expectError:    false,
			expectedStored: 0, // Schema validation failure - no signals stored
			expectedFailed: 1, // One signal failed validation
		},
		{
			name:      "empty_signals_array",
			accountID: adminAccount.ID,
			endpoint: testSignalEndpoint{
				isnSlug:          adminISN.Slug,
				signalTypeSlug:   adminSignalType.Slug,
				signalTypeSemVer: adminSignalType.SemVer,
			},
			payloadFunc:    func() map[string]any { return createEmptySignalsPayload() },
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
			expectedStored: 0, // No response body expected for bad request
			expectedFailed: 0,
		},
		{
			name:      "multiple_signals_batch",
			accountID: adminAccount.ID,
			endpoint: testSignalEndpoint{
				isnSlug:          adminISN.Slug,
				signalTypeSlug:   adminSignalType.Slug,
				signalTypeSemVer: adminSignalType.SemVer,
			},
			payloadFunc: func() map[string]any {
				return createMultipleSignalsPayload([]string{"batch-001", "batch-002", "batch-003"})
			},
			expectedStatus: http.StatusOK,
			expectError:    false,
			expectedStored: 3,
			expectedFailed: 0,
		},
	}

	// Run table-driven tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var authToken string
			if tt.customAuthToken != "" {
				authToken = tt.customAuthToken
			} else if !tt.skipAuthToken {
				authToken = testEnv.createAuthToken(t, tt.accountID)
			}

			payload := tt.payloadFunc()

			response := submitSignalRequest(t, baseURL, payload, authToken, tt.endpoint)
			defer response.Body.Close()

			// Verify response status
			if response.StatusCode != tt.expectedStatus {
				logResponseDetails(t, response, tt.name)
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				return // Don't continue with further validation if status is wrong
			}

			if !tt.expectError || (tt.expectedStatus == http.StatusUnprocessableEntity) {
				var result map[string]any
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				// Verify summary section exists
				summary, ok := result["summary"]
				if !ok {
					t.Error("Missing summary in response")
					return
				}

				summaryMap, ok := summary.(map[string]any)
				if !ok {
					t.Error("Summary is not a map")
					return
				}

				// Verify stored_count
				var countMismatch bool

				if storedCount, ok := summaryMap["stored_count"]; ok {
					if storedCountFloat, ok := storedCount.(float64); ok {
						actualStored := int(storedCountFloat)
						if actualStored != tt.expectedStored {
							countMismatch = true
							t.Errorf("Expected stored_count %d, got %d", tt.expectedStored, actualStored)
						}
					} else {
						countMismatch = true
						t.Error("stored_count is not a number")
					}
				} else {
					countMismatch = true
					t.Error("Missing stored_count in summary")
				}

				// Verify failed_count
				if failedCount, ok := summaryMap["failed_count"]; ok {
					if failedCountFloat, ok := failedCount.(float64); ok {
						actualFailed := int(failedCountFloat)
						if actualFailed != tt.expectedFailed {
							countMismatch = true
							t.Errorf("Expected failed_count %d, got %d", tt.expectedFailed, actualFailed)
						}
					} else {
						countMismatch = true
						t.Error("failed_count is not a number")
					}
				} else {
					countMismatch = true
					t.Error("Missing failed_count in summary")
				}

				// Verify total_submitted matches stored + failed
				if totalSubmitted, ok := summaryMap["total_submitted"]; ok {
					if totalFloat, ok := totalSubmitted.(float64); ok {
						actualTotal := int(totalFloat)
						expectedTotal := tt.expectedStored + tt.expectedFailed
						if actualTotal != expectedTotal {
							countMismatch = true
							t.Errorf("Expected total_submitted %d (stored %d + failed %d), got %d",
								expectedTotal, tt.expectedStored, tt.expectedFailed, actualTotal)
						}
					}
				}

				// If there were count mismatches, show detailed response for debugging
				if countMismatch {
					t.Logf("=== Response Details for %s (Count Validation Failed) ===", tt.name)
					t.Logf("Status: %d %s", response.StatusCode, response.Status)
					t.Logf("Headers: %v", response.Header)

					// Re-marshal the parsed result to show the response body
					if responseBody, err := json.MarshalIndent(result, "", "  "); err == nil {
						t.Logf("Body: %s", string(responseBody))
					} else {
						t.Logf("Body: %+v", result)
					}
					t.Logf("=== End Response Details ===")
				}
			}
		})
	}

}

// submitSignalRequest submits a signal payload with authentication
func submitSignalRequest(t *testing.T, baseURL string, payload map[string]any, token string, endpoint testSignalEndpoint) *http.Response {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	url := fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s/signals",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to submit signals: %v", err)
	}

	return resp
}

// searchPublicSignals searches for signals on public ISNs (no authentication required)
func searchPublicSignals(t *testing.T, baseURL string, endpoint testSignalEndpoint) *http.Response {
	url := fmt.Sprintf("%s/api/public/isn/%s/signal_types/%s/v%s/signals/search",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	q := req.URL.Query()
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	oneHourFromNow := now.Add(1 * time.Hour)
	q.Add("start_date", oneHourAgo.Format(time.RFC3339))
	q.Add("end_date", oneHourFromNow.Format(time.RFC3339))
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to search public signals: %v", err)
	}

	return resp
}

// searchPrivateSignals searches for signals on private ISNs (authentication required)
func searchPrivateSignals(t *testing.T, baseURL string, endpoint testSignalEndpoint, token string) *http.Response {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s/signals/search",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	q := req.URL.Query()
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	oneHourFromNow := now.Add(1 * time.Hour)
	q.Add("start_date", oneHourAgo.Format(time.RFC3339))
	q.Add("end_date", oneHourFromNow.Format(time.RFC3339))
	req.URL.RawQuery = q.Encode()

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to search private signals: %v", err)
	}

	return resp
}

// TestSignalSearch tests that the search signals endpoint returns the correct number of signals and that accounts only see signals they have access to
func TestSignalSearch(t *testing.T) {
	ctx := context.Background()

	testDB := setupTestDatabase(t, ctx)

	testEnv := setupTestEnvironment(testDB)

	// create test data:
	//
	// there are two private ISNs (owner and admin) with 1 signal each
	// owner can see all signals
	// admin can see the signal on the admin ISN, since they created it, and can't see the owner signal (auth error)
	// member is granted access to the admin ISN and can see the admin signal but can't see the owner signal (auth error)
	//
	// there is 1 pulic ISN with 1 signal -- all accounts can see this signal without auth

	t.Log("Creating test data...")

	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@search.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "admin", "user", "admin@search.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@search.com")

	ownerISN := createTestISN(t, ctx, testEnv.queries, "owner-search-isn", "Owner search ISN", ownerAccount.ID, "private")
	adminISN := createTestISN(t, ctx, testEnv.queries, "admin-search-isn", "Admin search ISN", adminAccount.ID, "private")
	publicISN := createTestISN(t, ctx, testEnv.queries, "public-search-isn", "Public search ISN", adminAccount.ID, "public")

	ownerSignalType := createTestSignalType(t, ctx, testEnv.queries, ownerISN.ID, "owner ISN search signal", "1.0.0")
	adminSignalType := createTestSignalType(t, ctx, testEnv.queries, adminISN.ID, "admin ISN search signal", "1.0.0")
	publicSignalType := createTestSignalType(t, ctx, testEnv.queries, publicISN.ID, "public ISN search signal", "1.0.0")

	// Grant write permissions to ISN owners so they can submit signals
	grantPermission(t, ctx, testEnv.queries, ownerISN.ID, ownerAccount.ID, "write")
	grantPermission(t, ctx, testEnv.queries, adminISN.ID, adminAccount.ID, "write")
	grantPermission(t, ctx, testEnv.queries, publicISN.ID, adminAccount.ID, "write")

	// Grant read permission to member for admin ISN
	grantPermission(t, ctx, testEnv.queries, adminISN.ID, memberAccount.ID, "read")

	// Create tokens after granting permissions so they include the ISN permissions in claims
	ownerToken := testEnv.createAuthToken(t, ownerAccount.ID)
	adminToken := testEnv.createAuthToken(t, adminAccount.ID)
	memberToken := testEnv.createAuthToken(t, memberAccount.ID)

	// note that the server must be started after the test data is created so the public isn cache is populated
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()
	t.Logf("✅ Server started at %s", baseURL)

	// end point configs
	ownerEndpoint := testSignalEndpoint{
		isnSlug:          ownerISN.Slug,
		signalTypeSlug:   ownerSignalType.Slug,
		signalTypeSemVer: ownerSignalType.SemVer,
	}

	adminEndpoint := testSignalEndpoint{
		isnSlug:          adminISN.Slug,
		signalTypeSlug:   adminSignalType.Slug,
		signalTypeSemVer: adminSignalType.SemVer,
	}

	publicEndpoint := testSignalEndpoint{
		isnSlug:          publicISN.Slug,
		signalTypeSlug:   publicSignalType.Slug,
		signalTypeSemVer: publicSignalType.SemVer,
	}

	// create owner ISN signal
	payload := createValidSignalPayload("owner-search-signal-001")
	response := submitSignalRequest(t, baseURL, payload, ownerToken, ownerEndpoint)
	if response.StatusCode != http.StatusOK {
		logResponseDetails(t, response, "owner ISN signal submission")
		response.Body.Close()
		t.Fatalf("Failed to submit signal to owner ISN: %d", response.StatusCode)
	}
	response.Body.Close()

	// create admin ISN signal
	payload = createValidSignalPayload("admin-search-signal-001")
	response = submitSignalRequest(t, baseURL, payload, adminToken, adminEndpoint)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to admin ISN: %d", response.StatusCode)
	}

	// create public ISN signal
	payload = createValidSignalPayload("public-search-signal-001")
	response = submitSignalRequest(t, baseURL, payload, adminToken, publicEndpoint)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to public ISN: %d", response.StatusCode)
	}

	// check signal on public isn can be searched w/o auth
	t.Run("public isn search", func(t *testing.T) {
		response := searchPublicSignals(t, baseURL, publicEndpoint)
		defer response.Body.Close()

		// Verify response status
		if response.StatusCode != http.StatusOK {
			logResponseDetails(t, response, "public ISN search")
			t.Errorf("Expected status %d, got %d", http.StatusOK, response.StatusCode)
			return
		}

		// Verify data access
		var signals []map[string]any
		if err := json.NewDecoder(response.Body).Decode(&signals); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(signals) != 1 {
			t.Errorf("Expected 1 signal, got %d", len(signals))
			return
		}
	})

	// verify private isns are not accessible via public endpoints
	t.Run("private isn blocked from public endpoint", func(t *testing.T) {
		// Attempt to access private ISN via public endpoint - should fail
		response := searchPublicSignals(t, baseURL, ownerEndpoint)
		defer response.Body.Close()

		// Should return 404 Not Found (ISN not in public cache)
		if response.StatusCode != http.StatusNotFound {
			logResponseDetails(t, response, "private ISN via public endpoint")
			t.Errorf("Private ISN possibly accessible via public endpoint: Expected 404, got %d", response.StatusCode)
			return
		}

	})

	// test private ISN search
	tests := []struct {
		name            string
		requesterToken  string
		targetEndpoint  testSignalEndpoint
		expectedStatus  int
		expectedSignals int
		shouldSeeData   bool
		description     string
	}{
		{
			name:            "owner_can_access_own_isn",
			requesterToken:  ownerToken,
			targetEndpoint:  ownerEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
			shouldSeeData:   true,
			description:     "Owner should access their own ISN",
		},
		{
			name:            "owner_can_access_admin_isn",
			requesterToken:  ownerToken,
			targetEndpoint:  adminEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
			shouldSeeData:   true,
			description:     "Owner should access admin ISN",
		},
		{
			name:            "admin_can_access_admin_isn",
			requesterToken:  adminToken,
			targetEndpoint:  adminEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
			shouldSeeData:   true,
			description:     "Admin should access their own ISN",
		},
		{
			name:            "admin_cannot_access_owner_isn",
			requesterToken:  adminToken,
			targetEndpoint:  ownerEndpoint,
			expectedStatus:  http.StatusForbidden,
			expectedSignals: 1,
			shouldSeeData:   false,
			description:     "Admin should not access owner's ISN",
		},
		{
			name:            "member_can_access_admin_isn",
			requesterToken:  memberToken,
			targetEndpoint:  adminEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
			shouldSeeData:   true,
			description:     "Member should access admin's ISN",
		},
		{
			name:            "member_cannot_access_owner_isn",
			requesterToken:  memberToken,
			targetEndpoint:  ownerEndpoint,
			expectedStatus:  http.StatusForbidden,
			expectedSignals: 1,
			shouldSeeData:   false,
			description:     "Member should not access owner's ISN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := searchPrivateSignals(t, baseURL, tt.targetEndpoint, tt.requesterToken)
			defer response.Body.Close()

			// Verify response status
			if response.StatusCode != tt.expectedStatus {
				logResponseDetails(t, response, tt.name)
				t.Errorf("Expected status %d, got %d. %s", tt.expectedStatus, response.StatusCode, tt.description)
				return
			}

			// For successful responses, verify data access
			if response.StatusCode == http.StatusOK && tt.shouldSeeData {
				var signals []map[string]any
				if err := json.NewDecoder(response.Body).Decode(&signals); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if len(signals) != tt.expectedSignals {
					t.Errorf("Expected %d signals, got %d", tt.expectedSignals, len(signals))
					return
				}
			}

			// For error responses, validate proper error structure
			if response.StatusCode >= 400 {
				var errorResponse map[string]any
				if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
					t.Fatalf("Failed to decode error response: %v", err)
				}

				// Verify error response contains required fields
				errorCode, hasErrorCode := errorResponse["error_code"]
				_, hasMessage := errorResponse["message"]

				if !hasErrorCode {
					t.Errorf("Error response missing 'error_code' field. Response: %+v", errorResponse)
				}
				if !hasMessage {
					t.Errorf("Error response missing 'message' field. Response: %+v", errorResponse)
				}

				// Verify error response only contains expected fields
				expectedFields := map[string]bool{"error_code": true, "message": true}
				for field := range errorResponse {
					if !expectedFields[field] {
						t.Errorf("Unexpected field '%s' in error response - potential data leakage. Response: %+v", field, errorResponse)
					}
				}

				if response.StatusCode == http.StatusForbidden {
					// Verify forbidden responses use the correct error code
					if errorCode != "forbidden" {
						t.Errorf("Expected error_code 'forbidden' for 403 response, got '%v'", errorCode)
					}
				}
			}
		})
	}
}

// TestCORS tests that protected endpoints respect ALLOWED_ORIGINS configuration and that untrusted origins are blocked
func TestCORS(t *testing.T) {
	t.Run("protected_endpoints_respect_allowed_origins", func(t *testing.T) {
		// Configure specific allowed origin
		t.Setenv("ALLOWED_ORIGINS", "https://trusted-app.example.com")

		ctx := context.Background()
		testDB := setupTestDatabase(t, ctx)
		testEnv := setupTestEnvironment(testDB)
		testURL := getTestDatabaseURL()
		baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
		defer stopServer()

		// Test trusted origin is allowed
		testCORSOrigin(t, baseURL, "/api/accounts", "https://trusted-app.example.com", true)

		// Test untrusted origin is blocked
		testCORSOrigin(t, baseURL, "/api/accounts", "https://malicious-site.com", false)
	})
}

// testCORSOrigin tests CORS behavior for a specific origin on a specific endpoint
func testCORSOrigin(t *testing.T, baseURL, endpoint, origin string, shouldAllow bool) {
	t.Helper()

	// make a preflight request with an Origin header and check he Access-Control-Allow-Origin response header
	req, err := http.NewRequest("OPTIONS", baseURL+endpoint, nil)
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

	if shouldAllow {
		if corsOrigin != origin {
			t.Errorf("Expected origin %s to be allowed, but got Access-Control-Allow-Origin: '%s'", origin, corsOrigin)
		}
	} else {
		// For disallowed origins, the header should be empty or not present
		if corsOrigin != "" && corsOrigin != "null" {
			t.Errorf("Origin %s should be blocked but got Access-Control-Allow-Origin: '%s'", origin, corsOrigin)
		}
	}
}
