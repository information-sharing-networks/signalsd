//go:build integration

package integration

// TestSharedSignalTypeIsolation verifies ISN isolation when the same signal type is
// registered on multiple ISNs.

import (
	"context"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

func TestSharedSignalTypeIsolation(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	// One account with access to both ISNs.
	account := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@sharedtype.com")

	alphaISN := createTestISN(t, ctx, testEnv.queries, "alpha-shared-isn", "Alpha ISN", account.ID, "private")
	betaISN := createTestISN(t, ctx, testEnv.queries, "beta-shared-isn", "Beta ISN", account.ID, "private")

	// create the signal type and register with Alpha
	sharedSignalType := createTestSignalType(t, ctx, testEnv.queries, alphaISN.ID, "Shared Signal Type", "", signalsd.ContentKindJSON)
	// add to Beta
	addSignalTypeToIsn(t, ctx, testEnv.queries, betaISN.ID, sharedSignalType.ID)

	grantPermission(t, ctx, testEnv.queries, alphaISN.ID, account.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, betaISN.ID, account.ID, "read-write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to load schema cache: %v", err)
	}

	// Create token after granting permissions so ISN perms are included in claims.
	token := testEnv.getAccessToken(t, account.ID)

	alphaEndpoint := newTestSignalEndpoint(alphaISN, sharedSignalType)
	betaEndpoint := newTestSignalEndpoint(betaISN, sharedSignalType)

	// Submit one signal to Alpha
	const alphaSignalRef = "alpha-only-001"
	alphaSignalID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload(alphaSignalRef), token, alphaEndpoint)

	// --- Search isolation ---

	t.Run("alpha ISN search returns 1 signal", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, lastHourSearchParams()))
		if len(signals) != 1 {
			t.Errorf("Expected 1 signal in alpha ISN, got %d", len(signals))
		}
	})

	t.Run("beta ISN search returns 0 signals", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, betaEndpoint, token, lastHourSearchParams()))
		if len(signals) != 0 {
			t.Errorf("Signal leaked from alpha ISN into beta ISN search: got %d signals, want 0", len(signals))
		}
	})

	// --- Withdrawal isolation ---

	t.Run("withdrawal via wrong ISN returns 404", func(t *testing.T) {
		expectStatus(t, withdrawSignal(t, testEnv.baseURL, betaEndpoint, token, alphaSignalRef), http.StatusNotFound)
	})

	t.Run("signal in alpha ISN unaffected after failed cross-ISN withdrawal", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, lastHourSearchParams()))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal in alpha ISN after failed cross-ISN withdrawal, got %d", len(signals))
		}
		if signals[0]["is_withdrawn"] == true {
			t.Error("Signal was incorrectly withdrawn by the cross-ISN withdrawal attempt")
		}
	})

	t.Run("withdrawal via correct ISN succeeds", func(t *testing.T) {
		expectStatus(t, withdrawSignal(t, testEnv.baseURL, alphaEndpoint, token, alphaSignalRef), http.StatusNoContent)
	})

	// --- Cross-ISN correlation validation ---

	t.Run("signal from alpha ISN rejected as correlation target in beta ISN", func(t *testing.T) {
		payload := createValidSignalPayloadWithCorrelatedID("beta-cross-corr-001", alphaSignalID)
		submission := expectSubmissionResponse(t, submitCreateSignalRequest(t, testEnv.baseURL, payload, token, betaEndpoint), http.StatusUnprocessableEntity)

		if len(submission.Results) != 1 || len(submission.Results[0].FailedSignals) != 1 {
			t.Fatalf("Expected 1 failed signal, got %+v", submission)
		}
		if errorCode := submission.Results[0].FailedSignals[0].ErrorCode; errorCode != apperrors.ErrCodeInvalidCorrelationID.String() {
			t.Errorf("Expected error_code %s, got %s", apperrors.ErrCodeInvalidCorrelationID, errorCode)
		}
	})

	// --- Correlated signals deduplication ---

	t.Run("correlated signals search returns no duplicates", func(t *testing.T) {
		// Submit a fresh parent/child pair to alpha ISN (the original signal was withdrawn above).
		parentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("alpha-parent-001"), token, alphaEndpoint)
		submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithCorrelatedID("alpha-child-001", parentID), token, alphaEndpoint)

		params := lastHourSearchParams()
		params["include_correlated"] = "true"
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, params))

		parent := findSignalByLocalRef(signals, "alpha-parent-001")
		if parent == nil {
			t.Fatal("Parent signal not found in search results")
		}
		correlated, ok := parent["correlated_signals"].([]any)
		if !ok {
			t.Fatal("Expected correlated_signals on parent signal")
		}
		if len(correlated) != 1 {
			t.Errorf("Expected exactly 1 correlated signal on parent, got %d — possible duplicate rows from shared signal type fan-out", len(correlated))
		}
	})
}
