package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// PostgresStore stores document content in the document_contents table.
//
// Postgres can't stream BYTEA values, so each document is held in memory while it is stored or read
// (memory use is up to MAX_DOCUMENT_SIZE per concurrent upload or download).
type PostgresStore struct {
	queries *database.Queries
}

func NewPostgresStore(queries *database.Queries) *PostgresStore {
	return &PostgresStore{queries: queries}
}

func (s *PostgresStore) Put(ctx context.Context, accountID uuid.UUID, content io.Reader) (Key, int64, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return Key{}, 0, fmt.Errorf("could not read document: %w", err)
	}

	hash := sha256.Sum256(data)
	key := Key{
		AccountID: accountID,
		SHA256:    hex.EncodeToString(hash[:]),
	}

	err = s.queries.InsertDocumentContent(ctx, database.InsertDocumentContentParams{
		AccountID: key.AccountID,
		Sha256:    key.SHA256,
		Content:   data,
	})
	if err != nil {
		return Key{}, 0, fmt.Errorf("could not store document %s: %w", key.SHA256, err)
	}
	return key, int64(len(data)), nil
}

func (s *PostgresStore) Get(ctx context.Context, key Key) (io.ReadCloser, error) {
	content, err := s.queries.GetDocumentContent(ctx, database.GetDocumentContentParams{
		AccountID: key.AccountID,
		Sha256:    key.SHA256,
	})
	if err != nil {
		return nil, fmt.Errorf("could not get document %s: %w", key.SHA256, err)
	}
	// the content is already in memory so there is nothing to close - NopCloser satisfies the io.ReadCloser that streaming backends need
	return io.NopCloser(bytes.NewReader(content)), nil
}
