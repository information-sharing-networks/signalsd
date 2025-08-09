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
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

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

func createValidSignalPayloadWithCorrelatedID(localRef string, correlationID string) map[string]any {
	return map[string]any{
		"signals": []map[string]any{
			{
				"local_ref":      localRef,
				"correlation_id": correlationID,
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

// createMultipleSignalsPayloadPartialFailure creates a payload with multiple signals, one of which will fail schema validation
func createMultipleSignalsPayloadPartialFailure(localRefs []string) map[string]any {
	signals := make([]map[string]any, len(localRefs))

	for i, ref := range localRefs {
		if i == 1 {
			signals[i] = map[string]any{
				"local_ref": ref,
				"content": map[string]any{
					"invalid_field": "this should fail schema validation",
					// Missing required "test" field
				},
			}
			continue
		}
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

// Helper function to validate response counts
func validateResponseCounts(t *testing.T, auditTrail map[string]any, expectedStored, expectedFailed int, testName string) bool {
	t.Helper()

	summary, ok := auditTrail["summary"]
	if !ok {
		t.Errorf("Missing summary in response for %s", testName)
		return false
	}

	summaryMap, ok := summary.(map[string]any)
	if !ok {
		t.Errorf("Summary is not a map for %s", testName)
		return false
	}

	var countMismatch bool

	// Verify stored_count
	if storedCount, ok := summaryMap["stored_count"]; ok {
		if storedCountFloat, ok := storedCount.(float64); ok {
			actualStored := int(storedCountFloat)
			if actualStored != expectedStored {
				countMismatch = true
				t.Errorf("Expected stored_count %d, got %d for %s", expectedStored, actualStored, testName)
			}
		} else {
			countMismatch = true
			t.Errorf("stored_count is not a number for %s", testName)
		}
	} else {
		countMismatch = true
		t.Errorf("Missing stored_count in summary for %s", testName)
	}

	// Verify failed_count
	if failedCount, ok := summaryMap["failed_count"]; ok {
		if failedCountFloat, ok := failedCount.(float64); ok {
			actualFailed := int(failedCountFloat)
			if actualFailed != expectedFailed {
				countMismatch = true
				t.Errorf("Expected failed_count %d, got %d for %s", expectedFailed, actualFailed, testName)
			}
		} else {
			countMismatch = true
			t.Errorf("failed_count is not a number for %s", testName)
		}
	} else {
		countMismatch = true
		t.Errorf("Missing failed_count in summary for %s", testName)
	}

	return countMismatch
}

// submitCreateSignalRequest submits a signal payload with authentication
func submitCreateSignalRequest(t *testing.T, baseURL string, payload map[string]any, token string, endpoint testSignalEndpoint) *http.Response {
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

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			MaxIdleConns:      0,
		},
	}
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

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			MaxIdleConns:      0,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to search public signals: %v", err)
	}

	return resp
}

// searchPrivateSignals searches for signals on private ISNs with optional correlated signals
func searchPrivateSignals(t *testing.T, baseURL string, endpoint testSignalEndpoint, token string, includeWithdrawn bool, includeCorrelated bool) *http.Response {
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

	if includeWithdrawn {
		q.Add("include_withdrawn", "true")
	}

	if includeCorrelated {
		q.Add("include_correlated", "true")
	}

	req.URL.RawQuery = q.Encode()

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			MaxIdleConns:      0,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to search private signals: %v", err)
	}

	return resp
}

// withdrawSignal withdraws a signal by local reference
func withdrawSignal(t *testing.T, baseURL string, endpoint testSignalEndpoint, token, localRef string) *http.Response {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s/signals/withdraw",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	withdrawRequest := map[string]string{
		"local_ref": localRef,
	}

	jsonData, err := json.Marshal(withdrawRequest)
	if err != nil {
		t.Fatalf("Failed to marshal withdrawal request: %v", err)
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create withdrawal request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			MaxIdleConns:      0,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to withdraw signal: %v", err)
	}

	return resp
}

// get the id of the created signal (only works when one signal is submitted)
func getSignalIDFromCreateSignalResponse(t *testing.T, response map[string]any) string {
	t.Helper()

	results, ok := response["results"].(map[string]any)
	if !ok {
		t.Fatal("Failed to get results from response")
	}

	storedSignals, ok := results["stored_signals"].([]any)
	if !ok || len(storedSignals) == 0 {
		t.Fatal("No stored signals in response")
	}
	if len(storedSignals) == 0 {
		t.Fatal("No stored signals in response")
	}

	firstSignal, ok := storedSignals[0].(map[string]any)
	if !ok {
		t.Fatal("First stored signal is not a map")
	}

	signalID, ok := firstSignal["signal_id"].(string)
	if !ok {
		t.Fatal("Signal ID is not a string")
	}

	return signalID

}

// TestSignalSubmission tests the signal submission process end-to-end (starts a http server as a go routine)
// there are tests for standalone and correlated signals
func TestSignalSubmission(t *testing.T) {

	ctx := context.Background()

	testDB := setupTestDatabase(t, ctx)

	testEnv := setupTestEnvironment(testDB)

	// select database and start the signalsd server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()
	t.Logf("✅ Server started at %s", baseURL)

	// create test data (take care if updating as used by multiple test below)
	t.Log("Creating test data...")

	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@gmail.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "admin", "user", "admin@gmail.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@gmail.com")

	ownerISN := createTestISN(t, ctx, testEnv.queries, "owner-isn", "Owner ISN", ownerAccount.ID, "private")
	adminISN := createTestISN(t, ctx, testEnv.queries, "admin-isn", "Admin ISN", adminAccount.ID, "private")

	ownerSignalType := createTestSignalType(t, ctx, testEnv.queries, ownerISN.ID, "owner ISN signal", "1.0.0")
	adminSignalType := createTestSignalType(t, ctx, testEnv.queries, adminISN.ID, "admin ISN signal", "1.0.0")

	grantPermission(t, ctx, testEnv.queries, adminISN.ID, memberAccount.ID, "read")

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

	// run the first tests
	t.Run("signal submission", func(t *testing.T) {

		tests := []struct {
			name              string
			accountID         uuid.UUID
			endpoint          testSignalEndpoint
			payloadFunc       func() map[string]any
			expectedStatus    int
			expectedErrorCode string
			skipAuthToken     bool
			customAuthToken   string
			expectedStored    int // Expected stored_count in response
			expectedFailed    int // Expected failed_count in response
		}{
			// valid signal loads
			{
				name:           "successful_signal_submission_by_admin",
				accountID:      adminAccount.ID,
				endpoint:       adminEndpoint,
				payloadFunc:    func() map[string]any { return createValidSignalPayload("admin-test-001") },
				expectedStatus: http.StatusOK,
				expectedStored: 1,
				expectedFailed: 0,
			},
			{
				name:           "successful_signal_submission_by_owner",
				accountID:      ownerAccount.ID,
				endpoint:       adminEndpoint,
				payloadFunc:    func() map[string]any { return createValidSignalPayload("owner-test-001") },
				expectedStatus: http.StatusOK,
				expectedStored: 1,
				expectedFailed: 0,
			},
			{
				name:      "multiple_signals_payload",
				accountID: adminAccount.ID,
				endpoint:  adminEndpoint,
				payloadFunc: func() map[string]any {
					return createMultipleSignalsPayload([]string{"batch-001", "batch-002", "batch-003"})
				},
				expectedStatus: http.StatusOK,
				expectedStored: 3,
				expectedFailed: 0,
			},
			// request level failures
			{
				name:              "unauthorized_signal_submission_by_member",
				accountID:         memberAccount.ID,
				endpoint:          adminEndpoint,
				payloadFunc:       func() map[string]any { return createValidSignalPayload("member-test-001") },
				expectedStatus:    http.StatusForbidden,
				expectedErrorCode: "forbidden",
			},
			{
				name:              "no_auth",
				accountID:         adminAccount.ID,
				endpoint:          adminEndpoint,
				payloadFunc:       func() map[string]any { return createValidSignalPayload("invalid-auth-001") },
				expectedStatus:    http.StatusUnauthorized,
				expectedErrorCode: apperrors.ErrCodeAuthorizationFailure.String(),
				skipAuthToken:     true,
			},
			{
				name:              "invalid_auth_token",
				accountID:         adminAccount.ID,
				endpoint:          adminEndpoint,
				payloadFunc:       func() map[string]any { return createValidSignalPayload("invalid-auth-001") },
				expectedStatus:    http.StatusUnauthorized,
				expectedErrorCode: apperrors.ErrCodeAuthorizationFailure.String(),
				customAuthToken:   "invalid-token",
			},
			{
				name:              "empty_signals_array",
				accountID:         adminAccount.ID,
				endpoint:          adminEndpoint,
				payloadFunc:       func() map[string]any { return createEmptySignalsPayload() },
				expectedStatus:    http.StatusBadRequest,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			// valid request format but individual signal failures
			{
				name:           "schema_validation_failure",
				accountID:      adminAccount.ID,
				endpoint:       adminEndpoint,
				payloadFunc:    func() map[string]any { return createInvalidSignalPayload("invalid-schema-001") },
				expectedStatus: http.StatusUnprocessableEntity,
				expectedStored: 0,
				expectedFailed: 1,
			},
			{
				name:      "multiple_signals_payload_partial_failures",
				accountID: adminAccount.ID,
				endpoint:  adminEndpoint,
				payloadFunc: func() map[string]any {
					return createMultipleSignalsPayloadPartialFailure([]string{"batch-004", "batch-005", "batch-006"})
				},
				expectedStatus: http.StatusMultiStatus,
				expectedStored: 2,
				expectedFailed: 1,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var authToken string
				if tt.customAuthToken != "" {
					authToken = tt.customAuthToken
				} else if !tt.skipAuthToken {
					authToken = testEnv.createAuthToken(t, tt.accountID)
				}

				payload := tt.payloadFunc()

				response := submitCreateSignalRequest(t, baseURL, payload, authToken, tt.endpoint)
				defer response.Body.Close()

				// Verify response status
				if response.StatusCode != tt.expectedStatus {
					t.Errorf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
					return
				}

				// verify errors are handled correctly
				var auditTrail map[string]any
				if err := json.NewDecoder(response.Body).Decode(&auditTrail); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				//response level errors have an error_code/error_message response and do not have a summary audit response
				if response.StatusCode == http.StatusForbidden ||
					response.StatusCode == http.StatusUnauthorized ||
					response.StatusCode == http.StatusBadRequest {

					errorCode, ok := auditTrail["error_code"].(string)
					if !ok {
						t.Fatalf("Failed to get error code from response: %v", auditTrail)
					}

					if tt.expectedErrorCode == "" || tt.expectedErrorCode != errorCode {
						t.Errorf("Expected error code %s, got %s", tt.expectedErrorCode, errorCode)
					}
					return
				}

				// Verify stored_count
				var countMismatch bool
				countMismatch = validateResponseCounts(t, auditTrail, tt.expectedStored, tt.expectedFailed, tt.name)

				// If there were count mismatches, show detailed response for debugging
				if countMismatch {
					t.Logf("=== Response Details for %s (Count Validation Failed) ===", tt.name)
					t.Logf("Status: %d %s", response.StatusCode, response.Status)
					t.Logf("Headers: %v", response.Header)

					// Re-marshal the parsed result to show the response body
					if responseBody, err := json.MarshalIndent(auditTrail, "", "  "); err == nil {
						t.Logf("Body: %s", string(responseBody))
					} else {
						t.Logf("Body: %+v", auditTrail)
					}
					t.Logf("=== End Response Details ===")
				}
			})
		}
	})
	// run the correlated signal tests
	t.Run("correlated signal submission", func(t *testing.T) {

		ownerToken := testEnv.createAuthToken(t, ownerAccount.ID)
		adminToken := testEnv.createAuthToken(t, adminAccount.ID)

		// create a signal in the admin isn
		adminPayload := createValidSignalPayload("admin-correlation-test-signal-001")

		adminSignalResponse := submitCreateSignalRequest(t, baseURL, adminPayload, adminToken, adminEndpoint)
		defer adminSignalResponse.Body.Close()

		if adminSignalResponse.StatusCode != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, adminSignalResponse.StatusCode)
			return
		}

		var adminResponseBody map[string]any
		if err := json.NewDecoder(adminSignalResponse.Body).Decode(&adminResponseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// create a signal in the owner isn
		ownerPayload := createValidSignalPayload("owner-correlation-test-signal-001")

		ownerSignalResponse := submitCreateSignalRequest(t, baseURL, ownerPayload, ownerToken, ownerEndpoint)
		defer ownerSignalResponse.Body.Close()

		if ownerSignalResponse.StatusCode != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, ownerSignalResponse.StatusCode)
			return
		}

		var ownerResponseBody = map[string]any{}

		if err := json.NewDecoder(ownerSignalResponse.Body).Decode(&ownerResponseBody); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// get the newly created signal id from the response - these will be used as the correlated ids in the tests below
		adminSignalID := getSignalIDFromCreateSignalResponse(t, adminResponseBody)
		ownerSignalID := getSignalIDFromCreateSignalResponse(t, ownerResponseBody)

		// tests run as admin - can write to their own isn but not to the owner isn
		tests := []struct {
			name              string
			correlatedID      string
			accountID         uuid.UUID
			endpoint          testSignalEndpoint
			expectError       bool
			expectedStatus    int
			expectedErrorCode string
		}{
			{
				name:              "empty_correlation_id",
				endpoint:          adminEndpoint,
				accountID:         adminAccount.ID,
				expectError:       true,
				expectedStatus:    400,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			{
				name:              "malformed_correlation_id",
				endpoint:          adminEndpoint,
				accountID:         adminAccount.ID,
				correlatedID:      "not-a-uuid",
				expectError:       true,
				expectedStatus:    400,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			{
				name:              "random_correlation_id",
				endpoint:          adminEndpoint,
				accountID:         adminAccount.ID,
				correlatedID:      uuid.New().String(),
				expectError:       true,
				expectedStatus:    422,
				expectedErrorCode: apperrors.ErrCodeInvalidCorrelationID.String(),
			},

			{
				name:              "valid_correlation_id_different_isn",
				endpoint:          adminEndpoint,
				accountID:         adminAccount.ID,
				correlatedID:      ownerSignalID,
				expectError:       true,
				expectedStatus:    422,
				expectedErrorCode: apperrors.ErrCodeInvalidCorrelationID.String(),
			},
			{
				name:           "valid_correlation_id_same_isn",
				endpoint:       adminEndpoint,
				accountID:      adminAccount.ID,
				correlatedID:   adminSignalID,
				expectError:    false,
				expectedStatus: 200,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				localRef := "admin-correlation-test-signal-001"
				correlatedPayload := createValidSignalPayloadWithCorrelatedID(localRef, tt.correlatedID)

				authToken := testEnv.createAuthToken(t, tt.accountID)

				correlatedSignalResponse := submitCreateSignalRequest(t, baseURL, correlatedPayload, authToken, tt.endpoint)
				defer correlatedSignalResponse.Body.Close()
				var correlatedSignalResponseBody map[string]any
				if err := json.NewDecoder(correlatedSignalResponse.Body).Decode(&correlatedSignalResponseBody); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if tt.expectError {
					var errorCode string
					var ok bool
					if tt.expectedStatus != correlatedSignalResponse.StatusCode {
						t.Errorf("Expected status %d, got %d", tt.expectedStatus, correlatedSignalResponse.StatusCode)
						return
					}

					// 400 is for malformed requests - entire request rejected with standard error_code/errror_message response
					if correlatedSignalResponse.StatusCode == 400 {
						errorCode, ok = correlatedSignalResponseBody["error_code"].(string)
						if !ok {
							t.Fatalf("Failed to get error code from response: %v", correlatedSignalResponseBody)
						}
					} else { // 422 is for processing errors - each failed signal has its own error_code/error_message
						errorCode, ok = correlatedSignalResponseBody["results"].(map[string]any)["failed_signals"].([]any)[0].(map[string]any)["error_code"].(string)
						if !ok {
							t.Fatalf("Failed to get error code from response: %v", correlatedSignalResponseBody)
						}
					}

					if tt.expectedErrorCode != errorCode {
						t.Errorf("Expected error code %s, got %s", tt.expectedErrorCode, errorCode)
					}
					return
				}

				if !tt.expectError && correlatedSignalResponse.StatusCode != 200 {
					t.Errorf("Expected status %d, got %d", http.StatusOK, correlatedSignalResponse.StatusCode)
					return
				}

				// For successful correlation, verify database consistency
				if tt.name == "valid_correlation_id_same_isn" {
					t.Run("verify_correlation_stored_correctly", func(t *testing.T) {
						ctx := context.Background()

						// Get the correlated signal details from database
						correlatedSignal, err := testEnv.queries.GetSignalCorrelationDetails(ctx, database.GetSignalCorrelationDetailsParams{
							AccountID: tt.accountID,
							Slug:      tt.endpoint.signalTypeSlug,
							SemVer:    tt.endpoint.signalTypeSemVer,
							LocalRef:  localRef,
						})
						if err != nil {
							t.Errorf("Failed to get correlated signal from database: %v", err)
							return
						}

						// Verify correlation_id matches what we submitted
						expectedCorrelationID, err := uuid.Parse(adminSignalID)
						if err != nil {
							t.Errorf("Failed to parse admin signal ID: %v", err)
							return
						}

						if correlatedSignal.CorrelationID != expectedCorrelationID {
							t.Errorf("Expected correlation_id %s, got %s", expectedCorrelationID, correlatedSignal.CorrelationID)
						}

						// Verify the correlation_id is valid in this ISN
						isValid, err := testEnv.queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
							CorrelationID: correlatedSignal.CorrelationID,
							IsnSlug:       correlatedSignal.IsnSlug,
						})
						if err != nil {
							t.Errorf("ValidateCorrelationID query failed: %v", err)
						}
						if !isValid {
							t.Errorf("Stored correlation_id %s is not valid in ISN %s", correlatedSignal.CorrelationID, correlatedSignal.IsnSlug)
						}
					})
				}
			})
		}

	})

}

// check that isns/signal types marked as is_in_use = false can't be read or written.
func TestIsInUseStatus(t *testing.T) {

	ctx := context.Background()

	testDB := setupTestDatabase(t, ctx)

	testEnv := setupTestEnvironment(testDB)

	// select database and start the signalsd server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()
	t.Logf("✅ Server started at %s", baseURL)

	t.Log("Creating test data...")

	// admin automatically has write permission for adminISN

	// granted pemissions:
	// admin > write access to ownerISN
	// member > write access to admin isn
	// member > write access to owner isn
	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@isinuse.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "admin", "user", "admin@isinuse.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@isinuse.com")

	ownerISN := createTestISN(t, ctx, testEnv.queries, "owner-isinuse-isn", "Owner ISN", ownerAccount.ID, "private")
	adminISN := createTestISN(t, ctx, testEnv.queries, "admin-isinuse-isn", "Admin ISN", adminAccount.ID, "private")

	ownerSignalType := createTestSignalType(t, ctx, testEnv.queries, ownerISN.ID, "owner isinuse ISN signal", "1.0.0")
	adminSignalType := createTestSignalType(t, ctx, testEnv.queries, adminISN.ID, "admin isinuse ISN signal", "1.0.0")
	disabledSignalType := createTestSignalType(t, ctx, testEnv.queries, ownerISN.ID, "owner isinuse ISN signal (inactive)", "1.0.0")

	grantPermission(t, ctx, testEnv.queries, ownerISN.ID, adminAccount.ID, "write")
	grantPermission(t, ctx, testEnv.queries, adminISN.ID, memberAccount.ID, "read")
	grantPermission(t, ctx, testEnv.queries, ownerISN.ID, memberAccount.ID, "write")

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
	disabledEndpoint := testSignalEndpoint{
		isnSlug:          ownerISN.Slug,
		signalTypeSlug:   disabledSignalType.Slug,
		signalTypeSemVer: disabledSignalType.SemVer,
	}

	//disable the admin.ISN
	_, err := testEnv.queries.UpdateIsn(ctx, database.UpdateIsnParams{
		ID:         adminISN.ID,
		Detail:     adminISN.Detail,
		IsInUse:    false,
		Visibility: adminISN.Visibility,
	})
	if err != nil {
		t.Fatalf("could not mark ISN %v as not in use", err)
	}

	// create and disable a new signal type in the owner isn
	_, err = testEnv.queries.UpdateSignalTypeDetails(ctx, database.UpdateSignalTypeDetailsParams{
		ID:        disabledSignalType.ID,
		ReadmeURL: disabledSignalType.ReadmeURL,
		Detail:    disabledSignalType.Detail,
		IsInUse:   false,
	})
	if err != nil {
		t.Fatalf("could not set is_in_use field on signal_types reccord: %v", err)
		return
	}
	t.Run("read write tests", func(t *testing.T) {

		tests := []struct {
			name           string
			accountID      uuid.UUID
			endpoint       testSignalEndpoint
			action         string
			expectedStatus int
		}{
			// note the status code returned when an ISN is marked "not in use" is 'forbidden' and for Signal Types it is "not found"
			{
				name:           "disabled isn is not writeable by the isn owner",
				accountID:      adminAccount.ID,
				action:         "write",
				endpoint:       adminEndpoint,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:           "disabled isn is not writeable by the site owner",
				accountID:      ownerAccount.ID,
				action:         "write",
				endpoint:       adminEndpoint,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:           "disabled isn is not writeable by a member granted access",
				accountID:      memberAccount.ID,
				action:         "write",
				endpoint:       adminEndpoint,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:           "disabling an isn does not prevent writes to other isns",
				accountID:      adminAccount.ID,
				action:         "write",
				endpoint:       ownerEndpoint,
				expectedStatus: http.StatusOK,
			},
			{
				name:           "disabled isn is not readable by the isn owner",
				accountID:      adminAccount.ID,
				action:         "read",
				endpoint:       adminEndpoint,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:           "disabled isn is not readable by the site owner",
				accountID:      ownerAccount.ID,
				action:         "read",
				endpoint:       adminEndpoint,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:           "disabled isn is not readable by a member granted access",
				accountID:      memberAccount.ID,
				action:         "read",
				endpoint:       adminEndpoint,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:           "disabled signal type not writeble by isn owner",
				accountID:      adminAccount.ID,
				action:         "write",
				endpoint:       disabledEndpoint,
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "disabled signal type not writeble by site owner",
				accountID:      ownerAccount.ID,
				action:         "write",
				endpoint:       disabledEndpoint,
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "disabled signal type not writeble by member granted access",
				accountID:      memberAccount.ID,
				action:         "write",
				endpoint:       disabledEndpoint,
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "disabled signal type not readable by isn owner",
				accountID:      adminAccount.ID,
				action:         "read",
				endpoint:       disabledEndpoint,
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "disabled signal type not readable by site owner",
				accountID:      ownerAccount.ID,
				action:         "read",
				endpoint:       disabledEndpoint,
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "disabled signal type not readable by member granted access",
				accountID:      memberAccount.ID,
				action:         "read",
				endpoint:       disabledEndpoint,
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "disabling one signal type does not prevent writing another",
				accountID:      adminAccount.ID,
				action:         "write",
				endpoint:       ownerEndpoint,
				expectedStatus: http.StatusOK,
			},
			{
				name:           "disabling one signal type does not prevent reading another",
				accountID:      adminAccount.ID,
				action:         "read",
				endpoint:       ownerEndpoint,
				expectedStatus: http.StatusOK,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				authToken := testEnv.createAuthToken(t, tt.accountID)

				switch tt.action {
				case "write":

					payload := createValidSignalPayload("isinuse-isn-001")
					response := submitCreateSignalRequest(t, baseURL, payload, authToken, tt.endpoint)
					defer response.Body.Close()

					// Verify response status
					if response.StatusCode != tt.expectedStatus {
						t.Errorf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
						return
					}
				case "read":
					response := searchPrivateSignals(t, baseURL, tt.endpoint, authToken, false, false)
					defer response.Body.Close()

					// Verify response status
					if response.StatusCode != tt.expectedStatus {
						t.Errorf("Expected status %d, got %d. %s", tt.expectedStatus, response.StatusCode, tt.name)
						return
					}
				default:
					t.Fatalf("invalid action received in test %v", tt.action)
				}
			})
		}

	})

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
	ownerResponse := submitCreateSignalRequest(t, baseURL, payload, ownerToken, ownerEndpoint)
	defer ownerResponse.Body.Close()
	if ownerResponse.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to owner ISN: %d", ownerResponse.StatusCode)
	}

	// create admin ISN signal
	payload = createValidSignalPayload("admin-search-signal-001")
	adminResponse := submitCreateSignalRequest(t, baseURL, payload, adminToken, adminEndpoint)
	defer adminResponse.Body.Close()
	if adminResponse.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to admin ISN: %d", adminResponse.StatusCode)
	}

	// create public ISN signal
	payload = createValidSignalPayload("public-search-signal-001")
	publicResponse := submitCreateSignalRequest(t, baseURL, payload, adminToken, publicEndpoint)
	publicResponse.Body.Close()
	if publicResponse.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to public ISN: %d", publicResponse.StatusCode)
	}

	// check signal on public isn can be searched w/o auth
	t.Run("public isn search", func(t *testing.T) {
		response := searchPublicSignals(t, baseURL, publicEndpoint)
		defer response.Body.Close()

		// Verify response status
		if response.StatusCode != http.StatusOK {
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

		// check the email is empty - should not be shown in public isns
		if email, ok := signals[0]["email"].(string); ok && email != "" {
			t.Errorf("Found email %s in public isn search - emails are not to be shown in public ISNs searches", email)
		}
	})

	// verify private isns are not accessible via public endpoints
	t.Run("private isn blocked from public endpoint", func(t *testing.T) {
		// Attempt to access private ISN via public endpoint - should fail
		response := searchPublicSignals(t, baseURL, ownerEndpoint)
		defer response.Body.Close()

		// Should return 404 Not Found (ISN not in public cache)
		if response.StatusCode != http.StatusNotFound {
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
			response := searchPrivateSignals(t, baseURL, tt.targetEndpoint, tt.requesterToken, false, false)
			defer response.Body.Close()

			// Verify response status
			if response.StatusCode != tt.expectedStatus {
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
					t.Errorf("Failed to decode error response: %v", err)
					return
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

	// Test withdrawn signals are excluded from search results by default
	t.Run("withdrawn signals excluded from search", func(t *testing.T) {
		// First, verify the admin signal is visible in search
		response := searchPrivateSignals(t, baseURL, adminEndpoint, adminToken, false, false)
		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("Failed to search admin ISN before withdrawal: %d", response.StatusCode)
		}

		var signalsBeforeWithdrawal []map[string]any
		if err := json.NewDecoder(response.Body).Decode(&signalsBeforeWithdrawal); err != nil {
			t.Fatalf("Failed to decode search response before withdrawal: %v", err)
		}

		if len(signalsBeforeWithdrawal) != 1 {
			t.Fatalf("Expected 1 signal before withdrawal, got %d", len(signalsBeforeWithdrawal))
		}

		// Withdraw the admin signal
		withdrawResponse := withdrawSignal(t, baseURL, adminEndpoint, adminToken, "admin-search-signal-001")
		defer withdrawResponse.Body.Close()

		if withdrawResponse.StatusCode != http.StatusNoContent {
			t.Fatalf("Failed to withdraw signal: %d", withdrawResponse.StatusCode)
		}

		// Search again - should return no signals (withdrawn signal excluded by default)
		response = searchPrivateSignals(t, baseURL, adminEndpoint, adminToken, false, false)
		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("Failed to search admin ISN after withdrawal: %d", response.StatusCode)
		}

		var signalsAfterWithdrawal []map[string]any
		if err := json.NewDecoder(response.Body).Decode(&signalsAfterWithdrawal); err != nil {
			t.Fatalf("Failed to decode search response after withdrawal: %v", err)
		}

		if len(signalsAfterWithdrawal) != 0 {
			t.Errorf("Expected 0 signals after withdrawal (withdrawn signals should be excluded), got %d", len(signalsAfterWithdrawal))
		}

		// Search with include_withdrawn=true - should return the withdrawn signal
		response = searchPrivateSignals(t, baseURL, adminEndpoint, adminToken, true, false)
		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("Failed to search admin ISN with include_withdrawn=true: %d", response.StatusCode)
		}

		var signalsWithWithdrawn []map[string]any
		if err := json.NewDecoder(response.Body).Decode(&signalsWithWithdrawn); err != nil {
			t.Fatalf("Failed to decode search response with include_withdrawn=true: %v", err)
		}

		if len(signalsWithWithdrawn) != 1 {
			t.Errorf("Expected 1 signal with include_withdrawn=true, got %d", len(signalsWithWithdrawn))
		}

		// Verify the returned signal is marked as withdrawn
		if len(signalsWithWithdrawn) > 0 {
			signal := signalsWithWithdrawn[0]
			isWithdrawn, exists := signal["is_withdrawn"]
			if !exists {
				t.Error("Signal response missing 'is_withdrawn' field")
			} else if isWithdrawn != true {
				t.Errorf("Expected withdrawn signal to have is_withdrawn=true, got %v", isWithdrawn)
			}
		}
	})
}

// TestCorrelatedSignalsSearch tests the include_correlated functionality
func TestCorrelatedSignalsSearch(t *testing.T) {
	ctx := context.Background()

	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// select database and start the signalsd server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()
	t.Logf("✅ Server started at %s", baseURL)

	// create test data
	t.Log("Creating test data...")

	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@correlated.com")

	ownerISN := createTestISN(t, ctx, testEnv.queries, "owner-correlated-isn", "Owner correlated ISN", ownerAccount.ID, "private")

	ownerSignalType := createTestSignalType(t, ctx, testEnv.queries, ownerISN.ID, "Owner correlated signal", "1.0.0")

	ownerAuthToken := testEnv.createAuthToken(t, ownerAccount.ID)

	ownerEndpoint := testSignalEndpoint{
		isnSlug:          ownerISN.Slug,
		signalTypeSlug:   ownerSignalType.Slug,
		signalTypeSemVer: ownerSignalType.SemVer,
	}

	// create master-001 signal (will have two correlated signals)
	master001LocalRef := "master-001"
	payload := createValidSignalPayload(master001LocalRef)

	master001SignalResponse := submitCreateSignalRequest(t, baseURL, payload, ownerAuthToken, ownerEndpoint)
	defer master001SignalResponse.Body.Close()

	if master001SignalResponse.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to owner ISN: %d", master001SignalResponse.StatusCode)
	}

	// create master-002 (no correlated signals)
	master002LocalRef := "master-002"
	payload = createValidSignalPayload(master002LocalRef)

	master002SignalResponse := submitCreateSignalRequest(t, baseURL, payload, ownerAuthToken, ownerEndpoint)
	defer master002SignalResponse.Body.Close()
	if master002SignalResponse.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to owner ISN: %d", master002SignalResponse.StatusCode)
	}

	// get signal ID from master 1
	var master001SignalResponseBody map[string]any
	if err := json.NewDecoder(master001SignalResponse.Body).Decode(&master001SignalResponseBody); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	master001SignalID := getSignalIDFromCreateSignalResponse(t, master001SignalResponseBody)

	// create two signals that are correlated with master_001
	correlated001LocalRef := "item-002>master-001"
	payload = createValidSignalPayloadWithCorrelatedID(correlated001LocalRef, master001SignalID)

	correlated001SignalResponse := submitCreateSignalRequest(t, baseURL, payload, ownerAuthToken, ownerEndpoint)
	defer correlated001SignalResponse.Body.Close()
	if correlated001SignalResponse.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to owner ISN: %d", correlated001SignalResponse.StatusCode)
	}

	correlated002LocalRef := "item-003>master-001"
	payload = createValidSignalPayloadWithCorrelatedID(correlated002LocalRef, master001SignalID)

	correlated002SignalResponse := submitCreateSignalRequest(t, baseURL, payload, ownerAuthToken, ownerEndpoint)
	defer correlated002SignalResponse.Body.Close()
	if correlated002SignalResponse.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to owner ISN: %d", correlated002SignalResponse.StatusCode)
	}

	t.Run("search without correlated signals", func(t *testing.T) {
		response := searchPrivateSignals(t, baseURL, ownerEndpoint, ownerAuthToken, false, false)
		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("Failed to search signals: %d", response.StatusCode)
		}

		var signals []map[string]any
		if err := json.NewDecoder(response.Body).Decode(&signals); err != nil {
			t.Fatalf("Failed to decode search response: %v", err)
		}

		// Should have 4 signals (2 masters + 2 correlated)
		if len(signals) != 4 {
			t.Errorf("Expected 3 signals, got %d", len(signals))
		}
		for i := range signals {
			if signals[i]["correlated_signals"] != nil {
				t.Errorf("Expected no correlated signals when include_correlated=false, but found %d", len(signals[i]["correlated_signals"].([]any)))
			}
		}

	})
	t.Run("search including correlated signals", func(t *testing.T) {
		response := searchPrivateSignals(t, baseURL, ownerEndpoint, ownerAuthToken, false, true)
		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("Failed to search signals: %d", response.StatusCode)
		}

		var signals []map[string]any
		if err := json.NewDecoder(response.Body).Decode(&signals); err != nil {
			t.Fatalf("Failed to decode search response: %v", err)
		}

		// Should have 4 signals (2 masters + 2 correlated)
		if len(signals) != 4 {
			t.Errorf("Expected 3 signals, got %d", len(signals))
		}
		for i := range signals {
			localRef := signals[i]["local_ref"].(string)

			correlated_signals, ok := signals[i]["correlated_signals"].([]any)

			switch localRef {
			case master001LocalRef:
				if len(correlated_signals) != 2 {
					t.Errorf("Expected 2 correlated signals for %s, got %d", localRef, len(correlated_signals))
				}
			case master002LocalRef, correlated001LocalRef, correlated002LocalRef:
				if ok {
					t.Errorf("Expected 0 correlated signals for %s, got %d", localRef, len(correlated_signals))
				}
			default:
				t.Errorf("Unexpected local_ref in search results: %s", localRef)
			}
		}

	})
}
