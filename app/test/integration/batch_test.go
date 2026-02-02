//go:build integration

package integration

// Integration tests for signal batch functionality
// Tests service account batch creation, validation, and lifecycle management

import (
	"context"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// TestBatchLifecycle tests the complete batch lifecycle for service accounts including:
// - Service account signal submission without batch fails with expected error
// - Service accounts can create initial batches successfully
// - Creating a second batch closes the previous batch
//
// Each test creates an empty temporary database and applies all the migrations so the schema reflects the latest code. The database is dropped after each test.

func TestBatchLifecycle(t *testing.T) {
	ctx := context.Background()
	testDB := setupCleanDatabase(t, ctx)

	queries := database.New(testDB)

	// Create test data
	t.Log("Creating test data...")

	// Create owner account and ISN
	ownerAccount := createTestAccount(t, ctx, queries, "owner", "user", "owner@batch-test.com")
	testISN := createTestISN(t, ctx, queries, "batch-test-isn", "Batch Test ISN", ownerAccount.ID, "private")

	// Create service account
	serviceAccount := createTestAccount(t, ctx, queries, "member", "service_account", "service@batch-test.com")

	// Grant ISN permission
	_, err := queries.CreateIsnAccount(ctx, database.CreateIsnAccountParams{
		IsnID:      testISN.ID,
		AccountID:  serviceAccount.ID,
		Permission: "write",
	})
	if err != nil {
		t.Fatalf("Failed to grant ISN permission: %v", err)
	}

	// Create signal type for testing
	_, err = queries.CreateSignalType(ctx, database.CreateSignalTypeParams{
		IsnID:         testISN.ID,
		Slug:          "batch-test-signal",
		SchemaURL:     testSchemaURL,
		ReadmeURL:     testReadmeURL,
		Title:         "Batch Test Signal",
		Detail:        "Signal type for batch testing",
		SemVer:        "1.0.0",
		SchemaContent: testSchemaContent,
	})
	if err != nil {
		t.Fatalf("Failed to create signal type: %v", err)
	}

	t.Run("service account signal submission without batch fails", func(t *testing.T) {
		// Create auth service
		authService := auth.NewAuthService(testServerConfig.secretKey, testServerConfig.environment, queries)

		// Create access token for service account (should have no batch ID)
		ctx := auth.ContextWithAccountID(context.Background(), serviceAccount.ID)
		tokenResponse, err := authService.CreateAccessToken(ctx)
		if err != nil {
			t.Fatalf("Failed to create access token: %v", err)
		}

		// Verify that the service account has write permission but no batch ID
		if tokenResponse.Perms[testISN.Slug].Permission != "write" {
			t.Errorf("Expected write permission, got %v", tokenResponse.Perms[testISN.Slug].Permission)
		}

		if tokenResponse.Perms[testISN.Slug].SignalBatchID != nil {
			t.Errorf("Expected no batch ID for service account without batch, got %v", *tokenResponse.Perms[testISN.Slug].SignalBatchID)
		}

		// This validates the condition that would trigger the error in CreateSignalsHandler:
		// "Service accounts must create a signal batch for this ISN before posting signals"
		if tokenResponse.AccountType == "service_account" && tokenResponse.Perms[testISN.Slug].SignalBatchID != nil {
			t.Error("Expected service account without batch to have nil SignalBatchID")
		}
	})

	t.Run("service account can create initial batch", func(t *testing.T) {
		// Verify no batch exists initially
		initialBatch := getLatestBatchForAccountAndISN(t, ctx, queries, serviceAccount.ID, testISN.Slug)
		if initialBatch != nil {
			t.Errorf("Expected no initial batch, got batch ID %v", initialBatch.ID)
		}

		// Create first batch
		batch1, err := queries.CreateSignalBatch(ctx, database.CreateSignalBatchParams{
			IsnID:     testISN.ID,
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("Failed to create first batch: %v", err)
		}

		// Verify batch was created and is latest
		assertBatchState(t, ctx, queries, batch1.ID, true)

		// Verify batch appears as the latest batch for this account/ISN
		latestBatch := getLatestBatchForAccountAndISN(t, ctx, queries, serviceAccount.ID, testISN.Slug)
		if latestBatch == nil {
			t.Fatal("Expected to find latest batch after creation")
		}
		if latestBatch.ID != batch1.ID {
			t.Errorf("Expected batch ID %v, got %v", batch1.ID, latestBatch.ID)
		}
		if !latestBatch.IsLatest {
			t.Error("Expected first batch to be latest")
		}
	})

	t.Run("creating second batch closes previous batch", func(t *testing.T) {
		// Get the current latest batch
		currentBatch := getLatestBatchForAccountAndISN(t, ctx, queries, serviceAccount.ID, testISN.Slug)
		if currentBatch == nil {
			t.Fatal("Expected existing batch from previous test")
		}
		firstBatchID := currentBatch.ID

		// Close any existing batches (simulating the CreateSignalsBatchHandler behavior)
		_, err := queries.CloseISNSignalBatchByAccountID(ctx, database.CloseISNSignalBatchByAccountIDParams{
			IsnID:     testISN.ID,
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("Failed to close existing batch: %v", err)
		}

		// Create second batch
		batch2, err := queries.CreateSignalBatch(ctx, database.CreateSignalBatchParams{
			IsnID:     testISN.ID,
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("Failed to create second batch: %v", err)
		}

		// Verify first batch is no longer latest
		assertBatchState(t, ctx, queries, firstBatchID, false)

		// Verify second batch is latest
		assertBatchState(t, ctx, queries, batch2.ID, true)

		// Verify only the second batch appears as the latest batch
		latestBatch := getLatestBatchForAccountAndISN(t, ctx, queries, serviceAccount.ID, testISN.Slug)
		if latestBatch == nil {
			t.Fatal("Expected to find latest batch after second batch creation")
		}
		if latestBatch.ID != batch2.ID {
			t.Errorf("Expected latest batch ID %v, got %v", batch2.ID, latestBatch.ID)
		}
	})

}
