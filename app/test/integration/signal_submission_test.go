//go:build integration

package integration

// Signal submission tests: POST /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals
//
// - TestSignalSubmission: authentication, request validation and per-signal processing results
// - TestCorrelatedSignalSubmission: correlation_id handling
// - TestUnchangedSignalResubmission: resubmissions that don't change a signal return the latest version (no new version is created)
// - TestIsInUseStatus: signals can't be written or read when the ISN or signal type is disabled
// - TestWritesWithTokenIssuedBeforeSignalTypeDisabled: the write queries reject signals when the claims in the access token are out of date

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

// TestSignalSubmission checks
// - successful submissions by accounts with write access (ISN owner and siteadmin)
// - authentication and authorisation failures
// - malformed requests (the whole request is rejected with 400)
// - per-signal failures (valid requests where some or all signals fail schema validation)
func TestSignalSubmission(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	// the admin owns the ISN, the siteadmin can write to all ISNs and the member has read-only access
	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@submission.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@submission.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@submission.com")

	isn := createTestISN(t, ctx, testEnv.queries, "submission-isn", "Submission ISN", adminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "submission signal", "", signalsd.ContentKindJSON)
	grantPermission(t, ctx, testEnv.queries, isn.ID, memberAccount.ID, "read")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	endpoint := newTestSignalEndpoint(isn, signalType)

	adminToken := testEnv.getAccessToken(t, adminAccount.ID)
	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)
	memberToken := testEnv.getAccessToken(t, memberAccount.ID)
	expiredToken := createExpiredAccessToken(t, adminAccount.ID, testEnv.cfg.SecretKey)

	tests := []struct {
		name    string
		token   string
		payload map[string]any

		expectedStatus int

		// request level errors (the whole request is rejected)
		expectedErrorCode apperrors.ErrorCode

		// per-signal results (the request was processed)
		expectedStored   int
		expectedRejected int
	}{
		// successful submissions
		{
			name:           "ISN owner can submit a signal",
			token:          adminToken,
			payload:        createValidSignalPayload("admin-001"),
			expectedStatus: http.StatusOK,
			expectedStored: 1,
		},
		{
			name:           "siteadmin can submit a signal",
			token:          siteAdminToken,
			payload:        createValidSignalPayload("siteadmin-001"),
			expectedStatus: http.StatusOK,
			expectedStored: 1,
		},
		{
			name:  "multiple signals in one request",
			token: adminToken,
			payload: map[string]any{
				"batch_ref": "test-batch",
				"signals": []map[string]any{
					{"local_ref": "multi-001", "content": map[string]any{"test": "signal 1"}},
					{"local_ref": "multi-002", "content": map[string]any{"test": "signal 2"}},
					{"local_ref": "multi-003", "content": map[string]any{"test": "signal 3"}},
				},
			},
			expectedStatus: http.StatusOK,
			expectedStored: 3,
		},

		// authentication and authorisation
		{
			name:              "account without write permission is forbidden",
			token:             memberToken,
			payload:           createValidSignalPayload("member-001"),
			expectedStatus:    http.StatusForbidden,
			expectedErrorCode: apperrors.ErrCodeForbidden,
		},
		{
			name:              "missing access token",
			token:             "",
			payload:           createValidSignalPayload("no-token-001"),
			expectedStatus:    http.StatusUnauthorized,
			expectedErrorCode: apperrors.ErrCodeAuthorizationFailure,
		},
		{
			name:              "invalid access token",
			token:             "invalid-token",
			payload:           createValidSignalPayload("invalid-token-001"),
			expectedStatus:    http.StatusUnauthorized,
			expectedErrorCode: apperrors.ErrCodeAuthorizationFailure,
		},
		{
			name:              "expired access token",
			token:             expiredToken,
			payload:           createValidSignalPayload("expired-token-001"),
			expectedStatus:    http.StatusUnauthorized,
			expectedErrorCode: apperrors.ErrCodeAccessTokenExpired,
		},
		{
			name:              "malformed JWT",
			token:             "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.malformed.signature",
			payload:           createValidSignalPayload("malformed-jwt-001"),
			expectedStatus:    http.StatusUnauthorized,
			expectedErrorCode: apperrors.ErrCodeAuthorizationFailure,
		},

		// malformed requests
		{
			name:  "missing batch_ref",
			token: adminToken,
			payload: map[string]any{
				"signals": []map[string]any{{"local_ref": "test-001", "content": map[string]any{"test": "data"}}},
			},
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},
		{
			name:  "invalid batch_ref format",
			token: adminToken,
			payload: map[string]any{
				"batch_ref": "invalid batch ref!@#$",
				"signals":   []map[string]any{{"local_ref": "test-001", "content": map[string]any{"test": "data"}}},
			},
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},
		{
			name:              "missing signals array",
			token:             adminToken,
			payload:           map[string]any{"batch_ref": "test-batch"},
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},
		{
			name:              "empty signals array",
			token:             adminToken,
			payload:           map[string]any{"batch_ref": "test-batch", "signals": []map[string]any{}},
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},
		{
			name:  "signal without local_ref",
			token: adminToken,
			payload: map[string]any{
				"batch_ref": "test-batch",
				"signals":   []map[string]any{{"content": map[string]any{"test": "data"}}},
			},
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},
		{
			name:  "signal without content",
			token: adminToken,
			payload: map[string]any{
				"batch_ref": "test-batch",
				"signals":   []map[string]any{{"local_ref": "test-001"}},
			},
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},

		// per-signal failures
		// (the test schema requires a "test" field)
		{
			name:  "all signals fail schema validation",
			token: adminToken,
			payload: map[string]any{
				"batch_ref": "test-batch",
				"signals": []map[string]any{
					{"local_ref": "invalid-001", "content": map[string]any{"invalid_field": "no test field"}},
				},
			},
			expectedStatus:   http.StatusUnprocessableEntity,
			expectedRejected: 1,
		},
		{
			name:  "some signals fail schema validation",
			token: adminToken,
			payload: map[string]any{
				"batch_ref": "test-batch",
				"signals": []map[string]any{
					{"local_ref": "partial-001", "content": map[string]any{"test": "signal 1"}},
					{"local_ref": "partial-002", "content": map[string]any{"invalid_field": "no test field"}},
					{"local_ref": "partial-003", "content": map[string]any{"test": "signal 3"}},
				},
			},
			expectedStatus:   http.StatusMultiStatus,
			expectedStored:   2,
			expectedRejected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := submitCreateSignalRequest(t, testEnv.baseURL, tt.payload, tt.token, endpoint)

			if tt.expectedErrorCode != "" {
				expectErrorCode(t, response, tt.expectedStatus, tt.expectedErrorCode)
				return
			}

			submission := expectSubmissionResponse(t, response, tt.expectedStatus)
			if submission.Summary.StoredCount != tt.expectedStored || submission.Summary.RejectedCount != tt.expectedRejected {
				t.Errorf("Expected %d stored and %d rejected, got %d and %d: %+v",
					tt.expectedStored, tt.expectedRejected, submission.Summary.StoredCount, submission.Summary.RejectedCount, submission)
			}
		})
	}
}

// TestCorrelatedSignalSubmission checks that signals can only be correlated with an existing signal on the same ISN
func TestCorrelatedSignalSubmission(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@correlated-submission.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@correlated-submission.com")

	adminISN := createTestISN(t, ctx, testEnv.queries, "correlated-submission-isn", "Correlated submission ISN", adminAccount.ID, "private")
	otherISN := createTestISN(t, ctx, testEnv.queries, "correlated-submission-other-isn", "Correlated submission other ISN", siteAdminAccount.ID, "private")

	adminSignalType := createTestSignalType(t, ctx, testEnv.queries, adminISN.ID, "correlated submission signal", "", signalsd.ContentKindJSON)
	otherSignalType := createTestSignalType(t, ctx, testEnv.queries, otherISN.ID, "other ISN signal", "", signalsd.ContentKindJSON)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	adminToken := testEnv.getAccessToken(t, adminAccount.ID)
	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	adminEndpoint := newTestSignalEndpoint(adminISN, adminSignalType)
	otherEndpoint := newTestSignalEndpoint(otherISN, otherSignalType)

	// the signals that the test signals are correlated with
	masterSignalID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("master-001"), adminToken, adminEndpoint)
	otherISNSignalID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("other-isn-001"), siteAdminToken, otherEndpoint)

	tests := []struct {
		name          string
		localRef      string
		correlationID string

		expectedStatus int

		// 400: the whole request is rejected; 422: the signal is rejected (see failed_signals in the response)
		expectedErrorCode apperrors.ErrorCode
	}{
		{
			name:              "empty correlation_id",
			localRef:          "empty-correlation-001",
			correlationID:     "",
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},
		{
			name:              "malformed correlation_id",
			localRef:          "malformed-correlation-001",
			correlationID:     "not-a-uuid",
			expectedStatus:    http.StatusBadRequest,
			expectedErrorCode: apperrors.ErrCodeMalformedBody,
		},
		{
			name:              "correlation_id of a signal that does not exist",
			localRef:          "unknown-correlation-001",
			correlationID:     uuid.NewV7().String(),
			expectedStatus:    http.StatusUnprocessableEntity,
			expectedErrorCode: apperrors.ErrCodeInvalidCorrelationID,
		},
		{
			name:              "correlation_id of a signal on another ISN",
			localRef:          "other-isn-correlation-001",
			correlationID:     otherISNSignalID,
			expectedStatus:    http.StatusUnprocessableEntity,
			expectedErrorCode: apperrors.ErrCodeInvalidCorrelationID,
		},
		{
			name:           "correlation_id of a signal on the same ISN",
			localRef:       "same-isn-correlation-001",
			correlationID:  masterSignalID,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := createValidSignalPayloadWithCorrelatedID(tt.localRef, tt.correlationID)
			response := submitCreateSignalRequest(t, testEnv.baseURL, payload, adminToken, adminEndpoint)

			switch tt.expectedStatus {
			case http.StatusBadRequest:
				expectErrorCode(t, response, tt.expectedStatus, tt.expectedErrorCode)
				return
			case http.StatusUnprocessableEntity:
				submission := expectSubmissionResponse(t, response, tt.expectedStatus)
				if len(submission.Results) != 1 || len(submission.Results[0].FailedSignals) != 1 {
					t.Fatalf("Expected 1 failed signal, got %+v", submission)
				}
				if errorCode := submission.Results[0].FailedSignals[0].ErrorCode; errorCode != tt.expectedErrorCode.String() {
					t.Errorf("Expected error code %s, got %s", tt.expectedErrorCode, errorCode)
				}
				return
			}

			expectSubmissionResponse(t, response, tt.expectedStatus)

			// check the correlation was stored
			storedSignal, err := testEnv.queries.GetSignalCorrelationDetails(ctx, database.GetSignalCorrelationDetailsParams{
				AccountID:      adminAccount.ID,
				IsnSlug:        adminISN.Slug,
				SignalTypeSlug: adminSignalType.Slug,
				SemVer:         adminSignalType.SemVer,
				LocalRef:       tt.localRef,
			})
			if err != nil {
				t.Fatalf("Failed to get the correlated signal from the database: %v", err)
			}
			if storedSignal.CorrelationID.String() != tt.correlationID {
				t.Errorf("Expected correlation_id %s, got %s", tt.correlationID, storedSignal.CorrelationID)
			}
		})
	}
}

// TestUnchangedSignalResubmission checks a json signal resubmitted without changes returns the latest version with unchanged=true
// (no new version is created), and that changed resubmissions still create new versions
func TestUnchangedSignalResubmission(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@unchanged-resubmission.com")
	isn := createTestISN(t, ctx, testEnv.queries, "unchanged-resubmission-isn", "Unchanged resubmission ISN", adminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "resubmitted signal", "", signalsd.ContentKindJSON)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	adminToken := testEnv.getAccessToken(t, adminAccount.ID)
	endpoint := newTestSignalEndpoint(isn, signalType)

	// submit returns the stored signal from a submission of one signal
	submit := func(t *testing.T, payload map[string]any) handlers.StoredSignal {
		t.Helper()
		submission := expectSubmissionResponse(t, submitCreateSignalRequest(t, testEnv.baseURL, payload, adminToken, endpoint), http.StatusOK)
		if len(submission.Results) != 1 || len(submission.Results[0].StoredSignals) != 1 {
			t.Fatalf("Expected 1 stored signal, got %+v", submission)
		}
		return submission.Results[0].StoredSignals[0]
	}

	masterID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("master-001"), adminToken, endpoint)
	otherMasterID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("master-002"), adminToken, endpoint)

	first := submit(t, createValidSignalPayloadWithCorrelatedID("resubmitted-001", masterID))
	if first.VersionNumber != 1 || first.Unchanged {
		t.Fatalf("Expected version 1 (not unchanged), got %+v", first)
	}

	t.Run("an identical resubmission is unchanged", func(t *testing.T) {
		resubmitted := submit(t, createValidSignalPayloadWithCorrelatedID("resubmitted-001", masterID))
		if !resubmitted.Unchanged || resubmitted.SignalVersionID != first.SignalVersionID || resubmitted.VersionNumber != 1 {
			t.Errorf("Expected the existing version %s with unchanged=true, got %+v", first.SignalVersionID, resubmitted)
		}
	})

	t.Run("a resubmission without the correlation_id is unchanged", func(t *testing.T) {
		resubmitted := submit(t, createValidSignalPayloadWithContent("resubmitted-001", "valid content for simple schema"))
		if !resubmitted.Unchanged || resubmitted.SignalVersionID != first.SignalVersionID {
			t.Errorf("Expected the existing version %s with unchanged=true, got %+v", first.SignalVersionID, resubmitted)
		}
	})

	t.Run("the same content with different key order and whitespace is unchanged", func(t *testing.T) {
		payload := createValidSignalPayloadWithCorrelatedID("resubmitted-001", masterID)
		payload["signals"].([]map[string]any)[0]["content"] = json.RawMessage(`{   "test":    "valid content for simple schema"  }`)
		if resubmitted := submit(t, payload); !resubmitted.Unchanged {
			t.Errorf("Expected unchanged=true, got %+v", resubmitted)
		}
	})

	t.Run("a resubmission with different content creates a new version", func(t *testing.T) {
		changed := submit(t, createValidSignalPayloadWithContent("resubmitted-001", "amended content"))
		if changed.Unchanged || changed.VersionNumber != 2 {
			t.Errorf("Expected version 2 (not unchanged), got %+v", changed)
		}
	})

	t.Run("a resubmission with a different correlation_id creates a new version", func(t *testing.T) {
		payload := createValidSignalPayloadWithCorrelatedID("resubmitted-001", otherMasterID)
		payload["signals"].([]map[string]any)[0]["content"] = map[string]any{"test": "amended content"}
		if recorrelated := submit(t, payload); recorrelated.Unchanged || recorrelated.VersionNumber != 3 {
			t.Errorf("Expected version 3 (not unchanged), got %+v", recorrelated)
		}
	})

	t.Run("resubmitting a withdrawn signal reactivates it with a new version", func(t *testing.T) {
		expectStatus(t, withdrawSignal(t, testEnv.baseURL, endpoint, adminToken, "resubmitted-001"), http.StatusNoContent)

		payload := createValidSignalPayloadWithCorrelatedID("resubmitted-001", otherMasterID)
		payload["signals"].([]map[string]any)[0]["content"] = map[string]any{"test": "amended content"}
		if reactivated := submit(t, payload); reactivated.Unchanged || reactivated.VersionNumber != 4 {
			t.Errorf("Expected version 4 (not unchanged), got %+v", reactivated)
		}

		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, adminToken, map[string]string{"local_ref": "resubmitted-001"}))
		if len(signals) != 1 || signals[0]["is_withdrawn"] != false {
			t.Errorf("Expected the signal to be reactivated, got %v", signals)
		}
	})
}

// TestIsInUseStatus checks that no account can write or read signals when either
// - the ISN is disabled (isn.is_in_use = false), or
// - the signal type is disabled on the ISN (isn_signal_types.is_in_use = false)
//
// and that signals on other ISNs are unaffected.
func TestIsInUseStatus(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@isinuse.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@isinuse.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@isinuse.com")

	// the ISN that is disabled during the test, and an ISN that stays enabled
	disabledISN := createTestISN(t, ctx, testEnv.queries, "disabled-isn", "Disabled ISN", adminAccount.ID, "private")
	enabledISN := createTestISN(t, ctx, testEnv.queries, "enabled-isn", "Enabled ISN", siteAdminAccount.ID, "private")

	disabledSignalType := createTestSignalType(t, ctx, testEnv.queries, disabledISN.ID, "disabled ISN signal", "", signalsd.ContentKindJSON)
	enabledSignalType := createTestSignalType(t, ctx, testEnv.queries, enabledISN.ID, "enabled ISN signal", "", signalsd.ContentKindJSON)

	// give every account read/write access to both ISNs (the siteadmin has access to all ISNs and the admin owns the disabled ISN)
	grantPermission(t, ctx, testEnv.queries, enabledISN.ID, adminAccount.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, disabledISN.ID, memberAccount.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, enabledISN.ID, memberAccount.ID, "read-write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	disabledEndpoint := newTestSignalEndpoint(disabledISN, disabledSignalType)
	enabledEndpoint := newTestSignalEndpoint(enabledISN, enabledSignalType)

	accounts := []database.GetAccountByIDRow{siteAdminAccount, adminAccount, memberAccount}

	t.Run("ISN disabled", func(t *testing.T) {
		_, err := testEnv.queries.UpdateIsn(ctx, database.UpdateIsnParams{
			ID:         disabledISN.ID,
			Detail:     disabledISN.Detail,
			IsInUse:    false,
			Visibility: disabledISN.Visibility,
		})
		if err != nil {
			t.Fatalf("Failed to disable the ISN: %v", err)
		}

		for _, account := range accounts {
			// the token is created after the change because the ISN in-use status is included in the claims
			token := testEnv.getAccessToken(t, account.ID)
			localRef := "signal-" + account.AccountRole

			t.Run(account.AccountRole+" can still use the enabled ISN", func(t *testing.T) {
				response := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload(localRef), token, enabledEndpoint)
				expectStatus(t, response, http.StatusOK)

				response = searchPrivateSignals(t, testEnv.baseURL, enabledEndpoint, token, lastHourSearchParams())
				expectStatus(t, response, http.StatusOK)
			})

			t.Run(account.AccountRole+" can't use the disabled ISN", func(t *testing.T) {
				response := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload(localRef), token, disabledEndpoint)
				errorResponse := expectJSONResponse(t, response, http.StatusNotFound)
				if message := fmt.Sprint(errorResponse["message"]); !strings.Contains(message, "ISN not in use") {
					t.Errorf("Expected write error message to contain %q, got %q", "ISN not in use", message)
				}

				response = searchPrivateSignals(t, testEnv.baseURL, disabledEndpoint, token, lastHourSearchParams())
				errorResponse = expectJSONResponse(t, response, http.StatusNotFound)
				if message := fmt.Sprint(errorResponse["message"]); !strings.Contains(message, "ISN not in use") {
					t.Errorf("Expected search error message to contain %q, got %q", "ISN not in use", message)
				}
			})
		}
	})

	t.Run("signal type disabled on the ISN", func(t *testing.T) {
		// re-enable the ISN and disable the signal type on it
		_, err := testEnv.queries.UpdateIsn(ctx, database.UpdateIsnParams{
			ID:         disabledISN.ID,
			Detail:     disabledISN.Detail,
			IsInUse:    true,
			Visibility: disabledISN.Visibility,
		})
		if err != nil {
			t.Fatalf("Failed to enable the ISN: %v", err)
		}
		_, err = testEnv.queries.UpdateIsnSignalTypeStatus(ctx, database.UpdateIsnSignalTypeStatusParams{
			IsnID:        disabledISN.ID,
			SignalTypeID: disabledSignalType.ID,
			IsInUse:      false,
		})
		if err != nil {
			t.Fatalf("Failed to disable the signal type on the ISN: %v", err)
		}

		for _, account := range accounts {
			// the token is created after the change because the signal type in-use status is included in the claims
			token := testEnv.getAccessToken(t, account.ID)
			localRef := "signal-" + account.AccountRole

			t.Run(account.AccountRole+" can still use the enabled ISN", func(t *testing.T) {
				response := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload(localRef), token, enabledEndpoint)
				expectStatus(t, response, http.StatusOK)

				response = searchPrivateSignals(t, testEnv.baseURL, enabledEndpoint, token, lastHourSearchParams())
				expectStatus(t, response, http.StatusOK)
			})

			t.Run(account.AccountRole+" can't use the disabled signal type", func(t *testing.T) {
				response := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload(localRef), token, disabledEndpoint)
				errorResponse := expectJSONResponse(t, response, http.StatusNotFound)
				if message := fmt.Sprint(errorResponse["message"]); !strings.Contains(message, "signal type not in use") {
					t.Errorf("Expected write error message to contain %q, got %q", "signal type not in use", message)
				}

				response = searchPrivateSignals(t, testEnv.baseURL, disabledEndpoint, token, lastHourSearchParams())
				errorResponse = expectJSONResponse(t, response, http.StatusNotFound)
				if message := fmt.Sprint(errorResponse["message"]); !strings.Contains(message, "signal type not in use") {
					t.Errorf("Expected search error message to contain %q, got %q", "signal type not in use", message)
				}
			})
		}
	})
}

// TestWritesWithTokenIssuedBeforeSignalTypeDisabled checks signals are not stored when the signal type has been disabled
// on the ISN after the access token was issued.
//
// The access token claims say the signal type is in use until the token expires, so the requests pass the permission checks.
// The queries that create signals check the ISN and signal type are in use, and the signal is rejected with resource_not_found.
func TestWritesWithTokenIssuedBeforeSignalTypeDisabled(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "admin@stale-token.com")
	isn := createTestISN(t, ctx, testEnv.queries, "stale-token-isn", "Stale token ISN", adminAccount.ID, "private")
	jsonSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "stale token signal", "", signalsd.ContentKindJSON)
	documentSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "stale token document", "", signalsd.ContentKindDocument)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	// the token is issued while the signal types are enabled
	adminToken := testEnv.getAccessToken(t, adminAccount.ID)

	// route every json signal to the ISN
	setRoutingConfig(t, testEnv, adminToken, jsonSignalType, handlers.UpdateSignalRoutingConfigRequest{
		RoutingField: "test",
		RoutingRules: []handlers.SignalRoutingRule{
			{MatchPattern: "*valid*", Operator: "matches", IsnSlug: isn.Slug, Sequence: 1},
		},
	})
	if err := testEnv.routerCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh router cache: %v", err)
	}

	// disable both signal types on the ISN after the token was issued
	for _, signalType := range []database.SignalType{jsonSignalType, documentSignalType} {
		_, err := testEnv.queries.UpdateIsnSignalTypeStatus(ctx, database.UpdateIsnSignalTypeStatusParams{
			IsnID:        isn.ID,
			SignalTypeID: signalType.ID,
			IsInUse:      false,
		})
		if err != nil {
			t.Fatalf("Failed to disable signal type %s on the ISN: %v", signalType.Slug, err)
		}
	}

	t.Run("create signals", func(t *testing.T) {
		response := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload("stale-001"), adminToken, newTestSignalEndpoint(isn, jsonSignalType))
		submission := expectSubmissionResponse(t, response, http.StatusUnprocessableEntity)

		if len(submission.Results) != 1 || len(submission.Results[0].FailedSignals) != 1 {
			t.Fatalf("Expected 1 failed signal, got %+v", submission)
		}
		if errorCode := submission.Results[0].FailedSignals[0].ErrorCode; errorCode != apperrors.ErrCodeResourceNotFound.String() {
			t.Errorf("Expected error code %s, got %s", apperrors.ErrCodeResourceNotFound, errorCode)
		}
	})

	t.Run("signal router", func(t *testing.T) {
		response := submitRouteSignalsRequest(t, testEnv.baseURL, createValidSignalPayload("stale-002"), adminToken, jsonSignalType.Slug, jsonSignalType.SemVer)
		submission := expectSubmissionResponse(t, response, http.StatusUnprocessableEntity)

		if len(submission.Results) != 1 || len(submission.Results[0].FailedSignals) != 1 {
			t.Fatalf("Expected 1 failed signal, got %+v", submission)
		}
		if errorCode := submission.Results[0].FailedSignals[0].ErrorCode; errorCode != apperrors.ErrCodeResourceNotFound.String() {
			t.Errorf("Expected error code %s, got %s", apperrors.ErrCodeResourceNotFound, errorCode)
		}
	})

	t.Run("document upload", func(t *testing.T) {
		response := uploadDocumentRequest(t, testEnv.baseURL, adminToken, newTestSignalEndpoint(isn, documentSignalType), documentUpload{
			batchRef: "stale-batch", localRef: "stale-003", fileName: "bl.pdf", contentType: "application/pdf", content: []byte("%PDF-1.7 stale"),
		})
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})
}
