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
// accounts can see their own batches, and site admins can see any account's batches
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

	t.Run("batch test require authentication", func(t *testing.T) {
		response := getBatchStatusRequest(t, testEnv.baseURL, "", "test-batch")
		expectErrorCode(t, response, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure)
	})
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
