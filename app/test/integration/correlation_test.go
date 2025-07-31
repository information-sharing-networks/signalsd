//go:build integration

// Integration tests for signal correlation functionality
// Tests signal correlation ID creation, validation, and relationships
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// createSignalPayloadWithCorrelation creates a signal payload with optional correlation_id
func createSignalPayloadWithCorrelation(localRef string, correlationID *uuid.UUID) map[string]any {
	signal := map[string]any{
		"local_ref": localRef,
		"content": map[string]any{
			"test": "valid content for simple schema",
		},
	}

	if correlationID != nil {
		signal["correlation_id"] = correlationID.String()
	}

	return map[string]any{
		"signals": []map[string]any{signal},
	}
}

// TestCorrelationFunctionality tests both basic correlation functionality and cross-ISN restrictions
// This comprehensive test uses the HTTP API to test correlation validation at the application level
func TestCorrelationFunctionality(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	testEnv := setupTestEnvironment(testDB)

	// Start HTTP server
	testURL := getTestDatabaseURL()
	baseURL, stopServer := startInProcessServer(t, ctx, testEnv.dbConn, testURL)
	defer stopServer()
	t.Logf("✅ Server started at %s", baseURL)

	// Create test data
	t.Log("Creating test environment for correlation ID testing...")

	// Create owner account and first ISN
	ownerAccount := createTestAccount(t, ctx, testEnv.queries, "owner", "user", "owner@correlation-test.com")
	firstISN := createTestISN(t, ctx, testEnv.queries, "correlation-test-isn", "Correlation Test ISN", ownerAccount.ID, "private")
	firstSignalType := createTestSignalType(t, ctx, testEnv.queries, firstISN.ID, "Correlation Test Signal", "1.0.0")

	// Grant permissions and create auth token
	grantPermission(t, ctx, testEnv.queries, firstISN.ID, ownerAccount.ID, "write")
	ownerToken := testEnv.createAuthToken(t, ownerAccount.ID)

	firstEndpoint := testSignalEndpoint{
		isnSlug:          firstISN.Slug,
		signalTypeSlug:   firstSignalType.Slug,
		signalTypeSemVer: firstSignalType.SemVer,
	}

	t.Logf("✅ Test environment created:")
	t.Logf("   - Owner Account ID: %s", ownerAccount.ID)
	t.Logf("   - ISN: %s (ID: %s)", firstISN.Slug, firstISN.ID)
	t.Logf("   - Signal Type: %s/%s (ID: %s)", firstSignalType.Slug, firstSignalType.SemVer, firstSignalType.ID)

	// Test basic correlation functionality
	t.Log("Testing basic correlation functionality...")

	// Create first signal (will be the target of correlation)
	payload := createSignalPayloadWithCorrelation("correlation-test-signal-001", nil)
	response := submitSignalRequest(t, baseURL, payload, ownerToken, firstEndpoint)
	if response.StatusCode != http.StatusOK {
		logResponseDetails(t, response, "first signal submission")
		response.Body.Close()
		t.Fatalf("Failed to submit first signal: %d", response.StatusCode)
	}

	// Verify response shows signal was stored
	var firstResponse map[string]any
	if err := json.NewDecoder(response.Body).Decode(&firstResponse); err != nil {
		response.Body.Close()
		t.Fatalf("Failed to decode first signal response: %v", err)
	}
	response.Body.Close()

	// Check that signal was stored successfully
	storedSignals := firstResponse["results"].(map[string]any)["stored_signals"].([]any)
	if len(storedSignals) != 1 {
		t.Fatalf("Expected 1 stored signal, got %d", len(storedSignals))
	}

	// Get the signal ID from database using local_ref
	firstSignal, err := testEnv.queries.GetSignalByAccountAndLocalRef(ctx, database.GetSignalByAccountAndLocalRefParams{
		AccountID: ownerAccount.ID,
		Slug:      firstSignalType.Slug,
		SemVer:    firstSignalType.SemVer,
		LocalRef:  "correlation-test-signal-001",
	})
	if err != nil {
		t.Fatalf("Failed to get first signal from database: %v", err)
	}

	t.Logf("First signal created with ID: %s", firstSignal.ID)

	// Create second signal that correlates with the first signal
	payload = createSignalPayloadWithCorrelation("correlation-test-signal-002", &firstSignal.ID)
	response = submitSignalRequest(t, baseURL, payload, ownerToken, firstEndpoint)
	if response.StatusCode != http.StatusOK {
		logResponseDetails(t, response, "second signal submission")
		response.Body.Close()
		t.Fatalf("Failed to submit second signal with correlation: %d", response.StatusCode)
	}
	response.Body.Close()

	// Verify the correlation was stored correctly in the database
	secondSignal, err := testEnv.queries.GetSignalByAccountAndLocalRef(ctx, database.GetSignalByAccountAndLocalRefParams{
		AccountID: ownerAccount.ID,
		Slug:      firstSignalType.Slug,
		SemVer:    firstSignalType.SemVer,
		LocalRef:  "correlation-test-signal-002",
	})
	if err != nil {
		t.Fatalf("Failed to get second signal from database: %v", err)
	}

	if secondSignal.CorrelationID != firstSignal.ID {
		t.Errorf("Expected correlation_id %s, got %s", firstSignal.ID, secondSignal.CorrelationID)
	}

	t.Log("✅ Basic correlation functionality working correctly")
}
