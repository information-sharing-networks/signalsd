//go:build integration

package integration

// TestSharedSignalTypeIsolation verifies ISN isolation when the same signal type is
// registered on multiple ISNs.
//
// TestLocalRefReuseAcrossIsns verifies that a local_ref sent to two ISNs with a shared signal type
// creates two independent signals (local_ref is unique per account, ISN and signal type).

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

func TestLocalRefReuseAcrossIsns(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	// One account with access to both ISNs.
	account := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@localrefreuse.com")

	alphaISN := createTestISN(t, ctx, testEnv.queries, "alpha-reuse-isn", "Alpha ISN", account.ID, "private")
	betaISN := createTestISN(t, ctx, testEnv.queries, "beta-reuse-isn", "Beta ISN", account.ID, "private")

	// the json and event signal types are registered with both ISNs
	sharedSignalType := createTestSignalType(t, ctx, testEnv.queries, alphaISN.ID, "Reused Signal Type", "", signalsd.ContentKindJSON)
	addSignalTypeToIsn(t, ctx, testEnv.queries, betaISN.ID, sharedSignalType.ID)
	sharedEventType := createTestSignalType(t, ctx, testEnv.queries, alphaISN.ID, "Reused Event Type", "", signalsd.ContentKindEvent)
	addSignalTypeToIsn(t, ctx, testEnv.queries, betaISN.ID, sharedEventType.ID)

	grantPermission(t, ctx, testEnv.queries, alphaISN.ID, account.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, betaISN.ID, account.ID, "read-write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to load schema cache: %v", err)
	}

	token := testEnv.getAccessToken(t, account.ID)

	alphaEndpoint := newTestSignalEndpoint(alphaISN, sharedSignalType)
	betaEndpoint := newTestSignalEndpoint(betaISN, sharedSignalType)

	const reusedRef = "reused-ref-001"
	alphaSignalID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithContent(reusedRef, "alpha content"), token, alphaEndpoint)

	var betaSignalID string

	t.Run("identical content sent to another ISN creates a new signal", func(t *testing.T) {
		submission := expectSubmissionResponse(t, submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayloadWithContent(reusedRef, "alpha content"), token, betaEndpoint), http.StatusOK)
		if len(submission.Results) != 1 || len(submission.Results[0].StoredSignals) != 1 {
			t.Fatalf("Expected 1 stored signal, got %+v", submission)
		}
		stored := submission.Results[0].StoredSignals[0]
		if stored.Unchanged || stored.VersionNumber != 1 || stored.SignalID.String() == alphaSignalID {
			t.Errorf("Expected version 1 of a new signal (not unchanged, not alpha signal %s), got %+v", alphaSignalID, stored)
		}
		betaSignalID = stored.SignalID.String()
	})

	t.Run("new versions and correlations in the other ISN do not change the alpha signal", func(t *testing.T) {
		betaParentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("beta-parent-001"), token, betaEndpoint)

		payload := createValidSignalPayloadWithCorrelatedID(reusedRef, betaParentID)
		submission := expectSubmissionResponse(t, submitCreateSignalRequest(t, testEnv.baseURL, payload, token, betaEndpoint), http.StatusOK)
		if len(submission.Results) != 1 || len(submission.Results[0].StoredSignals) != 1 {
			t.Fatalf("Expected 1 stored signal, got %+v", submission)
		}
		stored := submission.Results[0].StoredSignals[0]
		if stored.SignalID.String() != betaSignalID || stored.VersionNumber != 2 {
			t.Errorf("Expected version 2 of beta signal %s, got %+v", betaSignalID, stored)
		}

		params := lastHourSearchParams()
		params["local_ref"] = reusedRef
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, params))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal in alpha ISN, got %d", len(signals))
		}
		if signals[0]["signal_id"] != alphaSignalID || signals[0]["version_number"] != float64(1) || signals[0]["correlation_id"] != nil {
			t.Errorf("Expected alpha signal %s at version 1 and uncorrelated, got %v", alphaSignalID, signals[0])
		}

		// the beta parent's correlated signals include the beta signal only
		params = lastHourSearchParams()
		params["signal_id"] = betaParentID
		params["include_correlated"] = "true"
		signals = expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, betaEndpoint, token, params))
		if len(signals) != 1 {
			t.Fatalf("Expected the beta parent signal, got %d signals", len(signals))
		}
		correlated, ok := signals[0]["correlated_signals"].([]any)
		if !ok || len(correlated) != 1 || correlated[0].(map[string]any)["signal_id"] != betaSignalID {
			t.Errorf("Expected beta signal %s as the only correlated signal, got %v", betaSignalID, signals[0]["correlated_signals"])
		}
	})

	t.Run("withdrawing the signal in the other ISN does not withdraw the alpha signal", func(t *testing.T) {
		expectStatus(t, withdrawSignal(t, testEnv.baseURL, betaEndpoint, token, reusedRef), http.StatusNoContent)

		params := lastHourSearchParams()
		params["local_ref"] = reusedRef
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, params))
		if len(signals) != 1 || signals[0]["is_withdrawn"] == true {
			t.Errorf("Expected the alpha signal to be unaffected, got %v", signals)
		}
	})

	t.Run("an event local_ref can be reused in another ISN", func(t *testing.T) {
		alphaEventEndpoint := newTestSignalEndpoint(alphaISN, sharedEventType)
		betaEventEndpoint := newTestSignalEndpoint(betaISN, sharedEventType)
		alphaConsignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("alpha-consignment-001"), token, alphaEndpoint)
		betaConsignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("beta-consignment-001"), token, betaEndpoint)

		approved := map[string]any{"occurred_at": "2026-09-27T14:02:00Z", "certificate_no": "EHC-001"}
		alphaEvent := expectStoredEvent(t, submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("reused-event-001", alphaConsignmentID, approved), token, alphaEventEndpoint))
		betaEvent := expectStoredEvent(t, submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("reused-event-001", betaConsignmentID, approved), token, betaEventEndpoint))
		if betaEvent.Unchanged || betaEvent.VersionNumber != 1 || betaEvent.SignalID == alphaEvent.SignalID {
			t.Errorf("Expected version 1 of a new event (not alpha event %s), got %+v", alphaEvent.SignalID, betaEvent)
		}
	})
}
