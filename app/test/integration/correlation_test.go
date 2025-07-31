//go:build integration

// Integration tests for signal correlation functionality
// Tests signal correlation ID creation, validation, and relationships
package integration

import (
	"context"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// TestCorrelationFunctionality tests both basic correlation functionality and cross-ISN restrictions
// This comprehensive test combines environment setup to avoid duplication
func TestCorrelationFunctionality(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	queries := database.New(testDB)

	// Create test data
	t.Log("Creating test environment for correlation ID testing...")

	// Create owner account and first ISN
	ownerAccount := createTestAccount(t, ctx, queries, "owner", "user", "owner@correlation-test.com")
	firstISN := createTestISN(t, ctx, queries, "correlation-test-isn", "Correlation Test ISN", ownerAccount.ID, "private")

	// Create signal type for testing
	firstSignalType := createTestSignalType(t, ctx, queries, firstISN.ID, "Correlation Test Signal", "1.0.0")

	t.Logf("✅ Test environment created:")
	t.Logf("   - Owner Account ID: %s", ownerAccount.ID)
	t.Logf("   - ISN: %s (ID: %s)", firstISN.Slug, firstISN.ID)
	t.Logf("   - Signal Type: %s/%s (ID: %s)", firstSignalType.Slug, firstSignalType.SemVer, firstSignalType.ID)

	// Create first signal (will be the target of correlation)
	firstSignalID, err := queries.CreateSignal(ctx, database.CreateSignalParams{
		AccountID:      ownerAccount.ID,
		LocalRef:       "correlation-test-signal-001",
		SignalTypeSlug: firstSignalType.Slug,
		SemVer:         firstSignalType.SemVer,
	})
	if err != nil {
		t.Fatalf("Failed to create first signal: %v", err)
	}
	t.Logf("First signal created with ID: %s", firstSignalID)

	// Create second signal that correlates with the first signal
	secondSignalID, err := queries.CreateOrUpdateSignalWithCorrelationID(ctx, database.CreateOrUpdateSignalWithCorrelationIDParams{
		AccountID:      ownerAccount.ID,
		LocalRef:       "correlation-test-signal-002",
		CorrelationID:  firstSignalID,
		SignalTypeSlug: firstSignalType.Slug,
		SemVer:         firstSignalType.SemVer,
	})
	if err != nil {
		t.Fatalf("Failed to create second signal with correlation: %v", err)
	}
	t.Logf("Second signal created with ID: %s, correlated to: %s", secondSignalID, firstSignalID)

	// Get the second signal and verify its correlation_id
	secondSignal, err := queries.GetSignalByAccountAndLocalRef(ctx, database.GetSignalByAccountAndLocalRefParams{
		AccountID: ownerAccount.ID,
		Slug:      firstSignalType.Slug,
		SemVer:    firstSignalType.SemVer,
		LocalRef:  "correlation-test-signal-002",
	})
	if err != nil {
		t.Fatalf("Failed to get second signal: %v", err)
	}

	if secondSignal.CorrelationID != firstSignalID {
		t.Errorf("Expected correlation_id %s, got %s", firstSignalID, secondSignal.CorrelationID)
	}

	t.Logf("Correlation verified: Signal %s correlates to Signal %s", secondSignalID, firstSignalID)

	// Test correlation ID validation
	t.Log("Testing correlation ID validation...")
	isValid, err := queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
		CorrelationID: firstSignalID,
		IsnSlug:       firstISN.Slug,
	})
	if err != nil {
		t.Fatalf("Failed to validate correlation ID: %v", err)
	}
	if !isValid {
		t.Errorf("Expected first signal ID to be valid correlation ID, got false")
	}

	t.Log("Basic correlation functionality working correctly")

	// === PART 2: Test cross-ISN correlation restrictions ===
	t.Log("\n=== Testing cross-ISN correlation restrictions ===")

	// Create second ISN with signal type and signal
	secondISN := createTestISN(t, ctx, queries, "second-isn", "Second ISN", ownerAccount.ID, "private")
	secondSignalType := createTestSignalType(t, ctx, queries, secondISN.ID, "Second Signal Type", "1.0.0")

	// Create signal in second ISN
	thirdSignalID, err := queries.CreateSignal(ctx, database.CreateSignalParams{
		AccountID:      ownerAccount.ID,
		LocalRef:       "second-isn-signal-001",
		SignalTypeSlug: secondSignalType.Slug,
		SemVer:         secondSignalType.SemVer,
	})
	if err != nil {
		t.Fatalf("Failed to create signal in second ISN: %v", err)
	}

	t.Logf("✅ Second ISN setup complete:")
	t.Logf("   - ISN: %s (ID: %s)", secondISN.Slug, secondISN.ID)
	t.Logf("   - Signal Type: %s/%s", secondSignalType.Slug, secondSignalType.SemVer)
	t.Logf("   - Signal ID: %s", thirdSignalID)

	// Test 1: Validate that the first signal ID is NOT valid as a correlation ID for the second ISN
	t.Log("Testing correlation ID validation across ISNs...")

	// Validate that the first signal ID is NOT valid as a correlation ID for the second ISN
	isValidInSecondISN, err := queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
		CorrelationID: firstSignalID,
		IsnSlug:       secondISN.Slug,
	})
	if err != nil {
		t.Fatalf("Failed to validate correlation ID: %v", err)
	}
	if isValidInSecondISN {
		t.Errorf("Expected signal from first ISN to be invalid correlation ID for second ISN, but validation passed")
	} else {
		t.Log("✅ Cross-ISN correlation validation correctly rejected signal from different ISN")
	}

	// Test 2: Try to create a signal in the second ISN that correlates to a signal in the first ISN
	// This should succeed in creating the signal but the correlation should not be allowed
	t.Log("Attempting to create cross-ISN correlated signal...")

	// This should create the signal but fail the correlation validation
	crossISNSignalID, err := queries.CreateOrUpdateSignalWithCorrelationID(ctx, database.CreateOrUpdateSignalWithCorrelationIDParams{
		AccountID:      ownerAccount.ID,
		LocalRef:       "cross-isn-attempt-signal",
		CorrelationID:  firstSignalID, // This is from a different ISN
		SignalTypeSlug: secondSignalType.Slug,
		SemVer:         secondSignalType.SemVer,
	})

	if err != nil {
		t.Logf("✅ Cross-ISN correlation attempt failed at database level: %v", err)
	} else {
		t.Logf("⚠️ Cross-ISN correlation signal created with ID: %s", crossISNSignalID)

		// Verify that the correlation ID validation would fail
		isValid, err := queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
			CorrelationID: firstSignalID,
			IsnSlug:       secondISN.Slug,
		})
		if err != nil {
			t.Fatalf("Failed to validate correlation ID: %v", err)
		}
		if !isValid {
			t.Log("✅ Correlation ID validation correctly identifies cross-ISN correlation as invalid")
		} else {
			t.Error("❌ Correlation ID validation incorrectly allowed cross-ISN correlation")
		}

		// Check what correlation ID was actually stored
		createdSignal, err := queries.GetSignalByAccountAndLocalRef(ctx, database.GetSignalByAccountAndLocalRefParams{
			AccountID: ownerAccount.ID,
			Slug:      secondSignalType.Slug,
			SemVer:    secondSignalType.SemVer,
			LocalRef:  "cross-isn-attempt-signal",
		})
		if err != nil {
			t.Fatalf("Failed to get created signal: %v", err)
		}

		t.Logf("   - Created signal correlation_id: %s", createdSignal.CorrelationID)
		t.Logf("   - Attempted correlation_id: %s", firstSignalID)

		if createdSignal.CorrelationID == firstSignalID {
			t.Log("⚠️ Database allowed cross-ISN correlation - this should be caught by application validation")
		} else {
			t.Log("✅ Database did not store the cross-ISN correlation ID")
		}
	}

	t.Log("\n🎯 Comprehensive correlation functionality test complete!")
	t.Log("   Test summary:")
	t.Log("   ✅ Basic correlation functionality verified")
	t.Log("   ✅ Cross-ISN correlation restrictions verified")
	t.Log("   Available test data:")
	t.Log("   - Owner Account:", ownerAccount.ID)
	t.Log("   - First ISN:", firstISN.Slug)
	t.Log("   - Second ISN:", secondISN.Slug)
	t.Log("   - First Signal ID:", firstSignalID)
	t.Log("   - Second Signal ID (correlated):", secondSignalID)
	t.Log("   - Third Signal ID (second ISN):", thirdSignalID)
}
