//go:build integration

package integration

// Integration tests for signal batch functionality.
// Batches are now account-scoped and identified by a sender-supplied batch_ref.
// There is no server-managed lifecycle (no is_latest, no explicit close).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

func TestBatches(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@batch-test.com")
	serviceAccount := createTestAccount(t, ctx, testEnv.queries, "member", "service_account", "service@batch-test.com")

	t.Run("attempting to add same batch ref twice returns same batch ID", func(t *testing.T) {
		ref := "daily-sync-2026-04-02"

		b1, err := testEnv.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
			BatchRef:  ref,
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("first upsert failed: %v", err)
		}

		b2, err := testEnv.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
			BatchRef:  ref,
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("second upsert failed: %v", err)
		}

		if b1.ID != b2.ID {
			t.Errorf("expected same batch UUID on second call, got %v and %v", b1.ID, b2.ID)
		}
		if b1.BatchRef != ref {
			t.Errorf("expected batch_ref %q, got %q", ref, b1.BatchRef)
		}
	})

	t.Run("different refs produce different batches for same account", func(t *testing.T) {
		b1, err := testEnv.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
			BatchRef:  "run-1",
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("first batch failed: %v", err)
		}

		b2, err := testEnv.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
			BatchRef:  "run-2",
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("second batch failed: %v", err)
		}

		if b1.ID == b2.ID {
			t.Error("expected different batch UUIDs for different refs")
		}
	})

	t.Run("same ref for different accounts produces separate batches", func(t *testing.T) {
		ref := "shared-ref"

		b1, err := testEnv.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
			BatchRef:  ref,
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("service account batch failed: %v", err)
		}

		b2, err := testEnv.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
			BatchRef:  ref,
			AccountID: siteAdminAccount.ID,
		})
		if err != nil {
			t.Fatalf("site admin batch failed: %v", err)
		}

		if b1.ID == b2.ID {
			t.Error("expected different batch UUIDs for different accounts with the same ref")
		}
	})
}

// TestBatchEndpoints tests the batch status and search endpoints:
//   - accounts can see their own batches, and site admins can see any account's batches
//   - batch status reports unresolved failures, including failures for signals that were never stored and signals
//     the signal router could not route
func TestBatchEndpoints(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@batch-test.com")
	writerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer@batch-test.com")
	otherAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "other@batch-test.com")

	isn := createTestISN(t, ctx, testEnv.queries, "batch-test-isn", "batch test ISN", siteAdminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "batch test signal", "", signalsd.ContentKindJSON)
	grantPermission(t, ctx, testEnv.queries, isn.ID, writerAccount.ID, "write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)
	writerToken := testEnv.getAccessToken(t, writerAccount.ID)
	otherToken := testEnv.getAccessToken(t, otherAccount.ID)

	// the writer submits a signal in batch "test-batch" (the batch_ref used by createValidSignalPayload)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("batch-signal-001"), writerToken, newTestSignalEndpoint(isn, signalType))

	t.Run("an account can get the status of its own batch", func(t *testing.T) {
		response := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "test-batch"), http.StatusOK)

		if response["batch_ref"] != "test-batch" {
			t.Errorf("Expected batch_ref test-batch, got %v", response["batch_ref"])
		}
		if response["contains_failures"] != false {
			t.Errorf("Expected contains_failures false, got %v", response["contains_failures"])
		}
	})

	t.Run("an account can't see another account's batch", func(t *testing.T) {
		// batch refs belong to an account - the other account has no batch with this ref
		response := getBatchStatusRequest(t, testEnv.baseURL, otherToken, "test-batch")
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("only site admins can get the status of another account's batch", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/batches/test-batch/status?account_id=%s", testEnv.baseURL, writerAccount.ID)

		expectErrorCode(t, getRequestWithToken(t, url, otherToken), http.StatusForbidden, apperrors.ErrCodeForbidden)
		expectJSONResponse(t, getRequestWithToken(t, url, siteAdminToken), http.StatusOK)
	})

	t.Run("search returns the account's batches", func(t *testing.T) {
		// the search requires a date range
		dateRange := lastHourSearchParams()
		searchURL := fmt.Sprintf("%s/api/batches/search?created_after=%s&created_before=%s",
			testEnv.baseURL, url.QueryEscape(dateRange["start_date"]), url.QueryEscape(dateRange["end_date"]))

		batches := expectBatchList(t, getRequestWithToken(t, searchURL, writerToken))
		if len(batches) != 1 || batches[0]["batch_ref"] != "test-batch" {
			t.Errorf("Expected the writer's test-batch, got %v", batches)
		}

		batches = expectBatchList(t, getRequestWithToken(t, searchURL, otherToken))
		if len(batches) != 0 {
			t.Errorf("Expected no batches for the other account, got %v", batches)
		}
	})

	t.Run("failures for signals that were never stored are reported", func(t *testing.T) {
		payload := map[string]any{
			"batch_ref": "never-stored-batch",
			"signals": []map[string]any{
				{"local_ref": "never-stored-001", "content": map[string]any{"invalid_field": "no test field"}},
			},
		}
		expectSubmissionResponse(t, submitCreateSignalRequest(t, testEnv.baseURL, payload, writerToken, newTestSignalEndpoint(isn, signalType)), http.StatusUnprocessableEntity)

		batchStatus := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "never-stored-batch"), http.StatusOK)
		failures := unresolvedFailures(t, batchStatus, isn.Slug)
		if len(failures) != 1 || failures[0]["local_ref"] != "never-stored-001" || failures[0]["error_code"] != apperrors.ErrCodeMalformedBody.String() {
			t.Errorf("Expected an unresolved malformed_body failure for never-stored-001, got %v", batchStatus)
		}
	})

	t.Run("failures resolved by a later successful submission are not reported", func(t *testing.T) {
		invalidPayload := map[string]any{
			"batch_ref": "resolved-batch",
			"signals": []map[string]any{
				{"local_ref": "resolved-001", "content": map[string]any{"invalid_field": "no test field"}},
			},
		}
		validPayload := map[string]any{
			"batch_ref": "resolved-batch",
			"signals": []map[string]any{
				{"local_ref": "resolved-001", "content": map[string]any{"test": "fixed"}},
			},
		}
		endpoint := newTestSignalEndpoint(isn, signalType)
		expectSubmissionResponse(t, submitCreateSignalRequest(t, testEnv.baseURL, invalidPayload, writerToken, endpoint), http.StatusUnprocessableEntity)
		expectSubmissionResponse(t, submitCreateSignalRequest(t, testEnv.baseURL, validPayload, writerToken, endpoint), http.StatusOK)

		batchStatus := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "resolved-batch"), http.StatusOK)
		if batchStatus["contains_failures"] != false {
			t.Errorf("Expected contains_failures false once the failure was resolved, got %v", batchStatus)
		}
	})

	t.Run("signals the signal router could not route are reported without an ISN", func(t *testing.T) {
		// the only routing rule does not match the signal content, so the signal can't be routed
		setRoutingConfig(t, testEnv, siteAdminToken, signalType, handlers.UpdateSignalRoutingConfigRequest{
			RoutingField: "test",
			RoutingRules: []handlers.SignalRoutingRule{
				{MatchPattern: "*no-match*", Operator: "matches", IsnSlug: isn.Slug, Sequence: 1},
			},
		})
		if err := testEnv.routerCache.Load(ctx); err != nil {
			t.Fatalf("Failed to refresh router cache: %v", err)
		}

		payload := map[string]any{
			"batch_ref": "unroutable-batch",
			"signals": []map[string]any{
				{"local_ref": "unroutable-001", "content": map[string]any{"test": "valid content"}},
			},
		}
		expectSubmissionResponse(t, submitRouteSignalsRequest(t, testEnv.baseURL, payload, writerToken, signalType.Slug, signalType.SemVer), http.StatusUnprocessableEntity)

		batchStatus := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "unroutable-batch"), http.StatusOK)
		failures := unresolvedFailures(t, batchStatus, "")
		if len(failures) != 1 || failures[0]["local_ref"] != "unroutable-001" {
			t.Errorf("Expected an unresolved failure for unroutable-001 with no ISN, got %v", batchStatus)
		}
	})

	t.Run("batch test require authentication", func(t *testing.T) {
		response := getBatchStatusRequest(t, testEnv.baseURL, "", "test-batch")
		expectErrorCode(t, response, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure)
	})
}

// unresolvedFailures returns the unresolved failures for the ISN in a batch status response ("" for signals the router could not route)
func unresolvedFailures(t *testing.T, batchStatus map[string]any, isnSlug string) []map[string]any {
	t.Helper()

	statuses, _ := batchStatus["batch_status"].([]any)
	for _, s := range statuses {
		status := s.(map[string]any)
		if status["isn_slug"] != isnSlug {
			continue
		}
		var failures []map[string]any
		rows, _ := status["unresolved_failures"].([]any)
		for _, row := range rows {
			failures = append(failures, row.(map[string]any))
		}
		return failures
	}
	return nil
}

// getRequestWithToken sends a GET request with the access token
func getRequestWithToken(t *testing.T, url, token string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	return response
}

// expectBatchList checks the response status is 200, closes the body and returns the decoded list of batches
func expectBatchList(t *testing.T, response *http.Response) []map[string]any {
	t.Helper()
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", response.StatusCode, body)
	}

	var batches []map[string]any
	if err := json.Unmarshal(body, &batches); err != nil {
		t.Fatalf("Failed to decode batches: %v: %s", err, body)
	}
	return batches
}
