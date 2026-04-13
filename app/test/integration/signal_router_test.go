//go:build integration

package integration

// tests
// - Request-level failures (400/401)
// - Record-level failures (207/422)
// - Successful routing to a single ISN
// - Successful routing to multiple ISNs in a single batch
// - Correlation ID routing
// - Response structure correctness

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
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

func TestRouteSignals(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	// the test signal type expects a single field called "test", e.g
	//{ "test": "Hello, world!" }
	//
	// the test uses two ISNs (isn-a and isn-b) share the same signal type.
	// Routing rules on the signal type are:
	//   "*value-a*"  -> isn-a
	//   "*value-b*"  -> isn-b
	//
	// These accounts are used to check the access controls in the router works properly:
	// - writerAccount has write permission on isn-a only.
	// - readOnlyAccount has read permission on isn-a only.
	// - siteAdminAccount has write permission on all ISNs.
	//
	// background: the router does not know the list of destination ISNs at the point the request is made,
	// so it has to implement its own checks to confirm that accounts have write access after
	// it has resolved the destination ISNs.

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@router-test.com")
	writerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer@router-test.com")
	readOnlyAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "readonly@router-test.com")

	siteAdminToken := testEnv.createAuthToken(t, siteAdminAccount.ID)

	isnA := createTestISN(t, ctx, testEnv.queries, "isn-a", "ISN A", siteAdminAccount.ID, "private")
	isnB := createTestISN(t, ctx, testEnv.queries, "isn-b", "ISN B", siteAdminAccount.ID, "private")

	// create the signal type and add it to isn-a
	signalType := createTestSignalType(t, ctx, testEnv.queries, isnA.ID, "router test signal", "")

	// add the signal type to isn-b
	if err := testEnv.queries.AddSignalTypeToIsn(ctx, database.AddSignalTypeToIsnParams{
		IsnID:        isnB.ID,
		SignalTypeID: signalType.ID,
	}); err != nil {
		t.Fatalf("Failed to add signal type to isn-b: %v", err)
	}

	grantPermission(t, ctx, testEnv.queries, isnA.ID, writerAccount.ID, "write")
	grantPermission(t, ctx, testEnv.queries, isnA.ID, readOnlyAccount.ID, "read")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("schemaCache.Load: %v", err)
	}

	// add routing rules via the admin API, then reload the router cache.
	setRoutingConfig(t, testEnv, siteAdminToken, signalType,
		handlers.UpdateSignalRoutingConfigRequest{
			RoutingField: "test",
			RoutingRules: []handlers.SignalRoutingRule{
				{MatchPattern: "*value-a*", Operator: "matches", IsnSlug: isnA.Slug, Sequence: 1},
				{MatchPattern: "*value-b*", Operator: "matches", IsnSlug: isnB.Slug, Sequence: 2},
			},
		})

	// refresh the cache
	if err := testEnv.routerCache.Load(ctx); err != nil {
		t.Fatalf("routerCache.Load: %v", err)
	}

	slug := signalType.Slug
	semVer := signalType.SemVer
	signalTypePath := fmt.Sprintf("%s/v%s", slug, semVer)

	// Request-level failure tests
	t.Run("request_level_failures", func(t *testing.T) {
		writerToken := testEnv.createAuthToken(t, writerAccount.ID)

		tests := []struct {
			name              string
			token             string
			payload           map[string]any
			expectedStatus    int
			expectedErrorCode string
		}{
			// check appropriate middleware is in place
			{
				name:              "no_auth_token",
				token:             "",
				payload:           routerPayload("batch-1", []map[string]any{routerSignal("s1", "value-a")}),
				expectedStatus:    http.StatusUnauthorized,
				expectedErrorCode: apperrors.ErrCodeAuthorizationFailure.String(),
			},
			{
				name:              "expired_token",
				token:             createExpiredAccessToken(t, writerAccount.ID, testEnv.cfg.SecretKey),
				payload:           routerPayload("batch-1", []map[string]any{routerSignal("s1", "value-a")}),
				expectedStatus:    http.StatusUnauthorized,
				expectedErrorCode: apperrors.ErrCodeAccessTokenExpired.String(),
			},
			// mandator fields
			{
				name:              "missing_batch_ref",
				token:             writerToken,
				payload:           map[string]any{"signals": []map[string]any{routerSignal("s1", "value-a")}},
				expectedStatus:    http.StatusBadRequest,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			{
				name:              "missing_signals_array",
				token:             writerToken,
				payload:           map[string]any{"batch_ref": "batch-1"},
				expectedStatus:    http.StatusBadRequest,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			{
				name:              "empty_signals_array",
				token:             writerToken,
				payload:           routerPayload("batch-1", []map[string]any{}),
				expectedStatus:    http.StatusBadRequest,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			{
				name:              "invalid_batch_ref_format",
				token:             writerToken,
				payload:           routerPayload("invalid batch ref!", []map[string]any{routerSignal("s1", "value-a")}),
				expectedStatus:    http.StatusBadRequest,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			{
				name:              "missing_local_ref",
				token:             writerToken,
				payload:           routerPayload("batch-1", []map[string]any{{"content": map[string]any{"test": "value-a"}}}),
				expectedStatus:    http.StatusBadRequest,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
			{
				name:              "missing_content",
				token:             writerToken,
				payload:           routerPayload("batch-1", []map[string]any{{"local_ref": "s1"}}),
				expectedStatus:    http.StatusBadRequest,
				expectedErrorCode: apperrors.ErrCodeMalformedBody.String(),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp := submitRouteSignalsRequest(t, testEnv.baseURL, tt.payload, tt.token, slug, semVer)
				defer resp.Body.Close()

				if resp.StatusCode != tt.expectedStatus {
					t.Errorf("status: want %d, got %d", tt.expectedStatus, resp.StatusCode)
					return
				}
				var errResp map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if errResp["error_code"] != tt.expectedErrorCode {
					t.Errorf("error_code: want %q, got %q", tt.expectedErrorCode, errResp["error_code"])
				}
			})
		}
	})

	//  Record-level failure tests (207 / 422)
	t.Run("record_level_failures", func(t *testing.T) {

		// Unroutable signals: no matching rule or bad correlation ID.
		unroutableTests := []struct {
			name              string
			signal            map[string]any
			expectedErrorCode string
		}{
			{
				name:              "no_matching_routing_rule",
				signal:            routerSignal("s-nomatch", "no-rule-will-match-this"),
				expectedErrorCode: apperrors.ErrCodeInvalidRequest.String(),
			},
			{
				name:              "correlation_id_not_found",
				signal:            routerSignalWithCorrelation("s-badcorr", uuid.New(), "value-a"),
				expectedErrorCode: apperrors.ErrCodeInvalidCorrelationID.String(),
			},
		}
		for _, tt := range unroutableTests {
			t.Run(tt.name, func(t *testing.T) {
				token := testEnv.createAuthToken(t, siteAdminAccount.ID)
				resp := submitRouteSignalsRequest(t, testEnv.baseURL,
					routerPayload("batch-1", []map[string]any{tt.signal}),
					token, slug, semVer)
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusUnprocessableEntity {
					t.Fatalf("status: want 422, got %d", resp.StatusCode)
				}
				result := decodeRouterResponse(t, resp)
				if len(result.UnroutableSignals) != 1 {
					t.Fatalf("unroutable_signals: want 1, got %d", len(result.UnroutableSignals))
				}
				if result.UnroutableSignals[0].ErrorCode != tt.expectedErrorCode {
					t.Errorf("error_code: want %q, got %q", tt.expectedErrorCode, result.UnroutableSignals[0].ErrorCode)
				}
			})
		}

		// Permission failures: signal routed to an ISN the account cannot write.
		permissionTests := []struct {
			name        string
			accountID   uuid.UUID
			testValue   string // routes to isn-a ("value-a") or isn-b ("value-b")
			resolvedISN string
		}{
			{
				name:        "no_write_permission_on_resolved_isn",
				accountID:   writerAccount.ID, // write on isn-a only; value-b routes to isn-b
				testValue:   "value-b",
				resolvedISN: isnB.Slug,
			},
			{
				name:        "read_only_account_cannot_write",
				accountID:   readOnlyAccount.ID, // read on isn-a only; value-a routes to isn-a
				testValue:   "value-a",
				resolvedISN: isnA.Slug,
			},
		}
		for _, tt := range permissionTests {
			t.Run(tt.name, func(t *testing.T) {
				token := testEnv.createAuthToken(t, tt.accountID)
				resp := submitRouteSignalsRequest(t, testEnv.baseURL,
					routerPayload("batch-"+tt.name, []map[string]any{routerSignal("s-"+tt.name, tt.testValue)}),
					token, slug, semVer)
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusUnprocessableEntity {
					t.Fatalf("status: want 422, got %d", resp.StatusCode)
				}
				result := decodeRouterResponse(t, resp)
				isnResult := findIsnResult(result.Results, tt.resolvedISN)
				if isnResult == nil {
					t.Fatalf("no result entry for %s", tt.resolvedISN)
				}
				if len(isnResult.FailedSignals) != 1 {
					t.Fatalf("failed_signals: want 1, got %d", len(isnResult.FailedSignals))
				}
				if isnResult.FailedSignals[0].ErrorCode != apperrors.ErrCodeForbidden.String() {
					t.Errorf("error_code: want %q, got %q", apperrors.ErrCodeForbidden.String(), isnResult.FailedSignals[0].ErrorCode)
				}
			})
		}

		t.Run("signal_type_not_in_use_on_resolved_isn", func(t *testing.T) {
			_, err := testEnv.queries.UpdateIsnSignalTypeStatus(ctx, database.UpdateIsnSignalTypeStatusParams{
				IsnID:        isnA.ID,
				SignalTypeID: signalType.ID,
				IsInUse:      false,
			})
			if err != nil {
				t.Fatalf("UpdateIsnSignalTypeStatus: %v", err)
			}
			t.Cleanup(func() {
				_, _ = testEnv.queries.UpdateIsnSignalTypeStatus(ctx, database.UpdateIsnSignalTypeStatusParams{
					IsnID: isnA.ID, SignalTypeID: signalType.ID, IsInUse: true,
				})
			})

			// Token must be fresh so claims reflect the disabled state.
			token := testEnv.createAuthToken(t, siteAdminAccount.ID)
			resp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-disabled-st", []map[string]any{routerSignal("s-disabled-st", "value-a")}),
				token, slug, semVer)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status: want 422, got %d", resp.StatusCode)
			}
			result := decodeRouterResponse(t, resp)
			isnResult := findIsnResult(result.Results, isnA.Slug)
			if isnResult == nil {
				t.Fatalf("no result entry for isn-a")
			}
			if len(isnResult.FailedSignals) != 1 {
				t.Fatalf("failed_signals: want 1, got %d", len(isnResult.FailedSignals))
			}
			if isnResult.FailedSignals[0].ErrorCode != apperrors.ErrCodeInvalidRequest.String() {
				t.Errorf("error_code: want %q, got %q", apperrors.ErrCodeInvalidRequest.String(), isnResult.FailedSignals[0].ErrorCode)
			}
		})

		t.Run("isn_not_in_use", func(t *testing.T) {
			// Disabling an ISN propagates in_use=false to all its signal types in the claims.
			_, err := testEnv.queries.UpdateIsn(ctx, database.UpdateIsnParams{
				ID:         isnA.ID,
				Detail:     "ISN A",
				IsInUse:    false,
				Visibility: "private",
			})
			if err != nil {
				t.Fatalf("UpdateIsn: %v", err)
			}
			t.Cleanup(func() {
				_, _ = testEnv.queries.UpdateIsn(ctx, database.UpdateIsnParams{
					ID: isnA.ID, Detail: "ISN A", IsInUse: true, Visibility: "private",
				})
			})

			// Token must be fresh so claims reflect the disabled ISN.
			token := testEnv.createAuthToken(t, siteAdminAccount.ID)
			resp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-disabled-isn", []map[string]any{routerSignal("s-disabled-isn", "value-a")}),
				token, slug, semVer)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status: want 422, got %d", resp.StatusCode)
			}
			result := decodeRouterResponse(t, resp)
			isnResult := findIsnResult(result.Results, isnA.Slug)
			if isnResult == nil {
				t.Fatalf("no result entry for isn-a")
			}
			if len(isnResult.FailedSignals) != 1 {
				t.Fatalf("failed_signals: want 1, got %d", len(isnResult.FailedSignals))
			}
			if isnResult.FailedSignals[0].ErrorCode != apperrors.ErrCodeForbidden.String() {
				t.Errorf("error_code: want %q, got %q", apperrors.ErrCodeForbidden.String(), isnResult.FailedSignals[0].ErrorCode)
			}
		})

		t.Run("schema_validation_failure", func(t *testing.T) {
			// Content must match a routing rule but violate the schema.
			// The test schema requires exactly one "test" field (additionalProperties: false).
			token := testEnv.createAuthToken(t, siteAdminAccount.ID)
			resp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-badschema", []map[string]any{
					{"local_ref": "s-badschema", "content": map[string]any{"test": "value-a", "extra_field": "not_allowed"}},
				}),
				token, slug, semVer)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status: want 422, got %d", resp.StatusCode)
			}
			result := decodeRouterResponse(t, resp)
			isnResult := findIsnResult(result.Results, isnA.Slug)
			if isnResult == nil {
				t.Fatalf("no result entry for isn-a")
			}
			if len(isnResult.FailedSignals) != 1 {
				t.Fatalf("failed_signals: want 1, got %d", len(isnResult.FailedSignals))
			}
			if isnResult.FailedSignals[0].ErrorCode != apperrors.ErrCodeMalformedBody.String() {
				t.Errorf("error_code: want %q, got %q", apperrors.ErrCodeMalformedBody.String(), isnResult.FailedSignals[0].ErrorCode)
			}
		})
	})

	// sucessful loads
	t.Run("successful_routing", func(t *testing.T) {

		// Pattern match to a single ISN.
		patternMatchTests := []struct {
			name        string
			testValue   string
			resolvedISN string
		}{
			{name: "to_isn_a", testValue: "value-a", resolvedISN: isnA.Slug},
			{name: "to_isn_b", testValue: "value-b", resolvedISN: isnB.Slug},
		}
		for _, tt := range patternMatchTests {
			t.Run("pattern_match_"+tt.name, func(t *testing.T) {
				token := testEnv.createAuthToken(t, siteAdminAccount.ID)
				localRef := "s-match-" + tt.name
				resp := submitRouteSignalsRequest(t, testEnv.baseURL,
					routerPayload("batch-"+tt.name, []map[string]any{routerSignal(localRef, tt.testValue)}),
					token, slug, semVer)
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status: want 200, got %d", resp.StatusCode)
				}
				result := decodeRouterResponse(t, resp)
				if result.Summary.StoredCount != 1 || result.Summary.RejectedCount != 0 {
					t.Errorf("summary: want stored=1 failed=0, got stored=%d failed=%d",
						result.Summary.StoredCount, result.Summary.RejectedCount)
				}
				isnResult := findIsnResult(result.Results, tt.resolvedISN)
				if isnResult == nil {
					t.Fatalf("no result entry for %s", tt.resolvedISN)
				}
				if isnResult.SignalTypePath != signalTypePath {
					t.Errorf("signal_type_path: want %q, got %q", signalTypePath, isnResult.SignalTypePath)
				}
				if len(isnResult.StoredSignals) != 1 {
					t.Fatalf("stored_signals: want 1, got %d", len(isnResult.StoredSignals))
				}
				stored := isnResult.StoredSignals[0]
				if stored.LocalRef != localRef {
					t.Errorf("local_ref: want %q, got %q", localRef, stored.LocalRef)
				}
				if stored.SignalID == uuid.Nil {
					t.Error("signal_id should not be nil")
				}
				if stored.SignalVersionID == uuid.Nil {
					t.Error("signal_version_id should not be nil")
				}
				if stored.VersionNumber != 1 {
					t.Errorf("version_number: want 1, got %d", stored.VersionNumber)
				}
			})
		}

		t.Run("mixed_batch_routes_to_both_isns", func(t *testing.T) {
			token := testEnv.createAuthToken(t, siteAdminAccount.ID)
			resp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-multi-isn", []map[string]any{
					routerSignal("s-multi-a", "value-a"),
					routerSignal("s-multi-b", "value-b"),
				}),
				token, slug, semVer)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status: want 200, got %d", resp.StatusCode)
			}
			result := decodeRouterResponse(t, resp)
			if result.Summary.TotalSubmitted != 2 || result.Summary.StoredCount != 2 || result.Summary.RejectedCount != 0 {
				t.Errorf("summary: want total=2 stored=2 failed=0, got total=%d stored=%d failed=%d",
					result.Summary.TotalSubmitted, result.Summary.StoredCount, result.Summary.RejectedCount)
			}
			for _, isnSlug := range []string{isnA.Slug, isnB.Slug} {
				isnResult := findIsnResult(result.Results, isnSlug)
				if isnResult == nil {
					t.Fatalf("no result entry for %s", isnSlug)
				}
				if len(isnResult.StoredSignals) != 1 {
					t.Errorf("%s stored_signals: want 1, got %d", isnSlug, len(isnResult.StoredSignals))
				}
			}
		})

		t.Run("multiple_signals_to_same_isn", func(t *testing.T) {
			// Exercises the isnResults grouping logic when more than one signal lands on the same ISN.
			token := testEnv.createAuthToken(t, siteAdminAccount.ID)
			resp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-multi-same", []map[string]any{
					routerSignal("s-same-1", "value-a"),
					routerSignal("s-same-2", "value-a"),
					routerSignal("s-same-3", "value-a"),
				}),
				token, slug, semVer)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status: want 200, got %d", resp.StatusCode)
			}
			result := decodeRouterResponse(t, resp)
			if result.Summary.TotalSubmitted != 3 || result.Summary.StoredCount != 3 {
				t.Errorf("summary: want total=3 stored=3, got total=%d stored=%d",
					result.Summary.TotalSubmitted, result.Summary.StoredCount)
			}
			isnResult := findIsnResult(result.Results, isnA.Slug)
			if isnResult == nil {
				t.Fatalf("no result entry for isn-a")
			}
			if len(isnResult.StoredSignals) != 3 {
				t.Errorf("stored_signals: want 3, got %d", len(isnResult.StoredSignals))
			}
		})

		t.Run("correlation_id_routing", func(t *testing.T) {
			// Seed a signal in isn-a via pattern match to get a signal ID.
			token := testEnv.createAuthToken(t, siteAdminAccount.ID)
			seedResp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-corr-seed", []map[string]any{routerSignal("s-corr-seed", "value-a")}),
				token, slug, semVer)
			if seedResp.StatusCode != http.StatusOK {
				seedResp.Body.Close()
				t.Fatalf("seed signal failed: %d", seedResp.StatusCode)
			}
			seedResult := decodeRouterResponse(t, seedResp)
			seedResp.Body.Close()

			isnResultA := findIsnResult(seedResult.Results, isnA.Slug)
			if isnResultA == nil || len(isnResultA.StoredSignals) == 0 {
				t.Fatalf("seed signal not stored in isn-a")
			}
			correlationID := isnResultA.StoredSignals[0].SignalID

			// Submit a correlated signal with content that would match isn-b — correlation should win.
			corrResp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-corr", []map[string]any{
					routerSignalWithCorrelation("s-corr", correlationID, "value-b"),
				}),
				token, slug, semVer)
			defer corrResp.Body.Close()

			if corrResp.StatusCode != http.StatusOK {
				t.Fatalf("status: want 200, got %d", corrResp.StatusCode)
			}
			corrResult := decodeRouterResponse(t, corrResp)
			isnResultA = findIsnResult(corrResult.Results, isnA.Slug)
			if isnResultA == nil || len(isnResultA.StoredSignals) != 1 {
				t.Errorf("expected 1 signal stored in isn-a via correlation, got %+v", isnResultA)
			}
		})

		t.Run("partial_success_mixed_batch", func(t *testing.T) {
			// One signal routes and stores OK (isn-a), one has no matching rule.
			token := testEnv.createAuthToken(t, siteAdminAccount.ID)
			resp := submitRouteSignalsRequest(t, testEnv.baseURL,
				routerPayload("batch-partial", []map[string]any{
					routerSignal("s-partial-ok", "value-a"),
					routerSignal("s-partial-fail", "no-match"),
				}),
				token, slug, semVer)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusMultiStatus {
				t.Fatalf("status: want 207, got %d", resp.StatusCode)
			}
			result := decodeRouterResponse(t, resp)
			if result.Summary.TotalSubmitted != 2 || result.Summary.StoredCount != 1 || result.Summary.UnroutableCount != 1 {
				t.Errorf("summary: want total=2 stored=1 unroutable=1, got total=%d stored=%d unroutable=%d",
					result.Summary.TotalSubmitted, result.Summary.StoredCount, result.Summary.UnroutableCount)
			}
			if len(result.UnroutableSignals) != 1 || result.UnroutableSignals[0].LocalRef != "s-partial-fail" {
				t.Errorf("unroutable_signals: expected s-partial-fail, got %+v", result.UnroutableSignals)
			}
		})
	})
}

// submitRouteSignalsRequest posts to the signal router endpoint.
func submitRouteSignalsRequest(t *testing.T, baseURL string, payload map[string]any, token string, signalTypeSlug, semVer string) *http.Response {
	t.Helper()
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	url := fmt.Sprintf("%s/api/router/signal-types/%s/v%s/signals", baseURL, signalTypeSlug, semVer)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonPayload))
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
		t.Fatalf("Failed to submit routed signals: %v", err)
	}
	return resp
}

// decodeRouterResponse decodes a SignalSubmissionResponse from an http.Response body.
func decodeRouterResponse(t *testing.T, resp *http.Response) handlers.SignalSubmissionResponse {
	t.Helper()
	var result handlers.SignalSubmissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode router response: %v", err)
	}
	return result
}

// routerPayload builds a minimal valid request body for the router endpoint.
func routerPayload(batchRef string, signals []map[string]any) map[string]any {
	return map[string]any{
		"batch_ref": batchRef,
		"signals":   signals,
	}
}

// routerSignal builds a single signal entry with content that matches the test schema (requires "test" field).
func routerSignal(localRef, testFieldValue string) map[string]any {
	return map[string]any{
		"local_ref": localRef,
		"content":   map[string]any{"test": testFieldValue},
	}
}

// routerSignalWithCorrelation builds a signal entry with a correlation_id.
func routerSignalWithCorrelation(localRef string, correlationID uuid.UUID, testFieldValue string) map[string]any {
	return map[string]any{
		"local_ref":      localRef,
		"correlation_id": correlationID,
		"content":        map[string]any{"test": testFieldValue},
	}
}

// findIsnResult returns the IsnResult for the given ISN slug, or nil if not present.
func findIsnResult(results []handlers.IsnResult, isnSlug string) *handlers.IsnResult {
	for i := range results {
		if results[i].IsnSlug == isnSlug {
			return &results[i]
		}
	}
	return nil
}
