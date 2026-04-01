//go:build integration

package integration

// Integration tests for signal batch functionality.
// Batches are now account-scoped and identified by a sender-supplied batch_ref.
// There is no server-managed lifecycle (no is_latest, no explicit close).

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

func TestBatchGetOrCreate(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@batch-test.com")
	serviceAccount := createTestAccount(t, ctx, testEnv.queries, "member", "service_account", "service@batch-test.com")

	t.Run("get or create returns same batch for same ref", func(t *testing.T) {
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

	t.Run("get by ref and account returns correct batch", func(t *testing.T) {
		ref := "lookup-test-" + uuid.New().String()[:8]

		created, err := testEnv.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
			BatchRef:  ref,
			AccountID: serviceAccount.ID,
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}

		found := getBatchByRef(t, ctx, testEnv.queries, serviceAccount.ID, ref)
		if found == nil {
			t.Fatal("expected to find batch by ref, got nil")
		}
		if found.ID != created.ID {
			t.Errorf("expected ID %v, got %v", created.ID, found.ID)
		}
	})

	t.Run("get by ref returns nil for unknown ref", func(t *testing.T) {
		result := getBatchByRef(t, ctx, testEnv.queries, serviceAccount.ID, "does-not-exist")
		if result != nil {
			t.Errorf("expected nil for unknown ref, got batch %v", result.ID)
		}
	})
}
