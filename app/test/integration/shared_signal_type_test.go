//go:build integration

package integration

// TestSharedSignalTypeIsolation verifies ISN isolation when the same signal type is
// registered on multiple ISNs.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
)

func TestSharedSignalTypeIsolation(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	// One account with access to both ISNs.
	account := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@sharedtype.com")

	alphaISN := createTestISN(t, ctx, testEnv.queries, "alpha-shared-isn", "Alpha ISN", account.ID, "private")
	betaISN := createTestISN(t, ctx, testEnv.queries, "beta-shared-isn", "Beta ISN", account.ID, "private")

	// create the signal type and register with Alpha
	sharedSignalType := createTestSignalType(t, ctx, testEnv.queries, alphaISN.ID, "Shared Signal Type", "")
	// add to Beta
	addSignalTypeToIsn(t, ctx, testEnv.queries, betaISN.ID, sharedSignalType.ID)

	grantPermission(t, ctx, testEnv.queries, alphaISN.ID, account.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, betaISN.ID, account.ID, "read-write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to load schema cache: %v", err)
	}

	// Create token after granting permissions so ISN perms are included in claims.
	token := testEnv.createAuthToken(t, account.ID)

	alphaEndpoint := testSignalEndpoint{
		isnSlug:          alphaISN.Slug,
		signalTypeSlug:   sharedSignalType.Slug,
		signalTypeSemVer: sharedSignalType.SemVer,
	}
	betaEndpoint := testSignalEndpoint{
		isnSlug:          betaISN.Slug,
		signalTypeSlug:   sharedSignalType.Slug,
		signalTypeSemVer: sharedSignalType.SemVer,
	}

	// Submit one signal to Alpha
	const alphaSignalRef = "alpha-only-001"
	submitResp := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload(alphaSignalRef), token, alphaEndpoint)
	var submitBody map[string]any
	if err := json.NewDecoder(submitResp.Body).Decode(&submitBody); err != nil {
		t.Fatalf("Failed to decode submit response: %v", err)
	}
	submitResp.Body.Close()
	if submitResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to submit signal to alpha ISN: %d", submitResp.StatusCode)
	}
	alphaSignalID := getSignalIDFromCreateSignalResponse(t, submitBody)

	// --- Search isolation ---
	t.Run("alpha ISN search returns 1 signal", func(t *testing.T) {
		resp := searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, false, false, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		var signals []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&signals); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}
		if len(signals) != 1 {
			t.Errorf("Expected 1 signal in alpha ISN, got %d", len(signals))
		}
	})

	t.Run("beta ISN search returns 0 signals", func(t *testing.T) {
		resp := searchPrivateSignals(t, testEnv.baseURL, betaEndpoint, token, false, false, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		var signals []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&signals); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}
		if len(signals) != 0 {
			t.Errorf("Signal leaked from alpha ISN into beta ISN search: got %d signals, want 0", len(signals))
		}
	})

	// --- Withdrawal isolation ---

	t.Run("withdrawal via wrong ISN returns 404", func(t *testing.T) {
		resp := withdrawSignal(t, testEnv.baseURL, betaEndpoint, token, alphaSignalRef)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 for cross-ISN withdrawal attempt, got %d", resp.StatusCode)
		}
	})

	t.Run("signal in alpha ISN unaffected after failed cross-ISN withdrawal", func(t *testing.T) {
		resp := searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, false, false, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		var signals []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&signals); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal in alpha ISN after failed cross-ISN withdrawal, got %d", len(signals))
		}
		if isWithdrawn, ok := signals[0]["is_withdrawn"].(bool); ok && isWithdrawn {
			t.Error("Signal was incorrectly withdrawn by the cross-ISN withdrawal attempt")
		}
	})

	t.Run("withdrawal via correct ISN succeeds", func(t *testing.T) {
		resp := withdrawSignal(t, testEnv.baseURL, alphaEndpoint, token, alphaSignalRef)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("Expected 204 for valid withdrawal, got %d", resp.StatusCode)
		}
	})

	// --- Cross-ISN correlation validation ---

	t.Run("signal from alpha ISN rejected as correlation target in beta ISN", func(t *testing.T) {
		payload := createValidSignalPayloadWithCorrelatedID("beta-cross-corr-001", alphaSignalID)
		resp := submitCreateSignalRequest(t, testEnv.baseURL, payload, token, betaEndpoint)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Expected 422 for cross-ISN correlation, got %d", resp.StatusCode)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		results, _ := body["results"].([]any)
		if len(results) == 0 {
			t.Fatal("Expected results array in response")
		}
		firstResult, _ := results[0].(map[string]any)
		failedSignals, _ := firstResult["failed_signals"].([]any)
		if len(failedSignals) == 0 {
			t.Fatal("Expected failed_signals in response")
		}
		firstFailed, _ := failedSignals[0].(map[string]any)
		if errorCode, _ := firstFailed["error_code"].(string); errorCode != apperrors.ErrCodeInvalidCorrelationID.String() {
			t.Errorf("Expected error_code %s, got %s", apperrors.ErrCodeInvalidCorrelationID.String(), errorCode)
		}
	})

	// --- Correlated signals deduplication ---

	t.Run("correlated signals search returns no duplicates", func(t *testing.T) {
		// Submit a fresh parent/child pair to alpha ISN (the original signal was withdrawn above).
		parentResp := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload("alpha-parent-001"), token, alphaEndpoint)
		var parentBody map[string]any
		if err := json.NewDecoder(parentResp.Body).Decode(&parentBody); err != nil {
			t.Fatalf("Failed to decode parent submit response: %v", err)
		}
		parentResp.Body.Close()
		if parentResp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to submit parent signal: %d", parentResp.StatusCode)
		}
		parentID := getSignalIDFromCreateSignalResponse(t, parentBody)

		childResp := submitCreateSignalRequest(t, testEnv.baseURL,
			createValidSignalPayloadWithCorrelatedID("alpha-child-001", parentID), token, alphaEndpoint)
		childResp.Body.Close()
		if childResp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to submit child signal: %d", childResp.StatusCode)
		}

		resp := searchPrivateSignals(t, testEnv.baseURL, alphaEndpoint, token, false, true, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		var signals []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&signals); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}

		for _, s := range signals {
			if s["local_ref"] == "alpha-parent-001" {
				correlated, ok := s["correlated_signals"].([]any)
				if !ok {
					t.Fatal("Expected correlated_signals on parent signal")
				}
				if len(correlated) != 1 {
					t.Errorf("Expected exactly 1 correlated signal on parent, got %d — possible duplicate rows from shared signal type fan-out", len(correlated))
				}
				return
			}
		}
		t.Error("Parent signal not found in search results")
	})
}
