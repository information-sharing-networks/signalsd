//go:build integration

package integration

// TestPostgresDocumentStore tests the postgres document store directly against a test database (no HTTP server)

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/documents"
)

func TestPostgresDocumentStore(t *testing.T) {
	ctx := context.Background()

	pool := setupTestDatabase(t)
	queries := database.New(pool)
	store := documents.NewPostgresStore(queries)

	// document content belongs to an account
	account1 := createTestAccount(t, ctx, queries, "member", "user", "account1@document-store.com")
	account2 := createTestAccount(t, ctx, queries, "member", "user", "account2@document-store.com")

	t.Run("Put returns the key and size of the content", func(t *testing.T) {
		key, size, err := store.Put(ctx, account1.ID, bytes.NewReader([]byte("hello")))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// sha256("hello")
		expectedSHA256 := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
		if key.SHA256 != expectedSHA256 {
			t.Errorf("Expected sha256 %s, got %s", expectedSHA256, key.SHA256)
		}
		if key.AccountID != account1.ID {
			t.Errorf("Expected account ID %s, got %s", account1.ID, key.AccountID)
		}
		if size != 5 {
			t.Errorf("Expected size 5, got %d", size)
		}
	})

	t.Run("stored content can be read back", func(t *testing.T) {
		content := []byte("%PDF-1.7 bill of lading")

		key, _, err := store.Put(ctx, account1.ID, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if got := readDocumentContent(t, ctx, store, key); !bytes.Equal(got, content) {
			t.Errorf("Expected %q, got %q", content, got)
		}
	})

	t.Run("a document of the maximum size can be stored", func(t *testing.T) {
		content := make([]byte, testMaxDocumentSize)
		if _, err := rand.Read(content); err != nil {
			t.Fatalf("Failed to create random content: %v", err)
		}

		key, size, err := store.Put(ctx, account1.ID, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if size != int64(len(content)) {
			t.Errorf("Expected size %d, got %d", len(content), size)
		}
		if got := readDocumentContent(t, ctx, store, key); !bytes.Equal(got, content) {
			t.Errorf("Content read back does not match the %d byte document that was stored (got %d bytes)", len(content), len(got))
		}
	})

	t.Run("storing the same content again is a no-op", func(t *testing.T) {
		content := []byte("commercial invoice")

		firstKey, _, err := store.Put(ctx, account1.ID, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("First Put failed: %v", err)
		}
		secondKey, _, err := store.Put(ctx, account1.ID, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Second Put failed: %v", err)
		}
		if firstKey != secondKey {
			t.Errorf("Expected the same key both times, got %v and %v", firstKey, secondKey)
		}
		if got := readDocumentContent(t, ctx, store, firstKey); !bytes.Equal(got, content) {
			t.Errorf("Expected %q, got %q", content, got)
		}
	})

	t.Run("the same content from different accounts is stored separately", func(t *testing.T) {
		content := []byte("packing list")

		key1, _, err := store.Put(ctx, account1.ID, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put for account 1 failed: %v", err)
		}
		key2, _, err := store.Put(ctx, account2.ID, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put for account 2 failed: %v", err)
		}
		if key1.SHA256 != key2.SHA256 {
			t.Errorf("Expected the same sha256 for the same content, got %s and %s", key1.SHA256, key2.SHA256)
		}
		if key1.AccountID == key2.AccountID {
			t.Errorf("Expected each key to belong to the account that stored the content, got %s for both", key1.AccountID)
		}

		// content is looked up by account and sha256, so each account can only read it back if it has its own copy
		if got := readDocumentContent(t, ctx, store, key1); !bytes.Equal(got, content) {
			t.Errorf("Expected account 1 to read back %q, got %q", content, got)
		}
		if got := readDocumentContent(t, ctx, store, key2); !bytes.Equal(got, content) {
			t.Errorf("Expected account 2 to read back %q, got %q", content, got)
		}
	})

	t.Run("getting content that was never stored returns an error", func(t *testing.T) {
		key := documents.Key{AccountID: account1.ID, SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}

		if _, err := store.Get(ctx, key); err == nil {
			t.Error("Expected an error getting content that was never stored")
		}
	})

	t.Run("content for an account that does not exist is rejected", func(t *testing.T) {
		if _, _, err := store.Put(ctx, uuid.NewV7(), bytes.NewReader([]byte("certificate of origin"))); err == nil {
			t.Error("Expected an error storing content for an account that does not exist")
		}
	})
}

// readDocumentContent reads the content stored under the key and fails the test if it can't be read
func readDocumentContent(t *testing.T, ctx context.Context, store documents.Store, key documents.Key) []byte {
	t.Helper()

	reader, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read document content: %v", err)
	}
	return content
}
