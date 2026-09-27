package documents

import (
	"context"
	"io"
	"uuid"
)

// Key identifies the content of a document: the account that uploaded it and the sha256 of the content.
// Keys are returned by Store.Put, and are rebuilt from the document metadata when the content is read.
type Key struct {
	AccountID uuid.UUID
	SHA256    string // lowercase hex
}

// Store holds the content of documents.
//
// The interface is designed for streaming: Put reads the content from a reader and computes the key as it goes,
// and Get returns a reader.
type Store interface {
	// Put reads the content, stores it and returns its key and size in bytes.
	// Storing content the account has already stored is a no-op.
	// The caller must limit the size of the content - e.g. the upload handler wraps the file in http.MaxBytesReader(MAX_DOCUMENT_SIZE)
	Put(ctx context.Context, accountID uuid.UUID, content io.Reader) (Key, int64, error)

	// Get returns the content stored under the key. The caller must close the reader.
	// Content is always stored before the signal that refers to it, so failing to find it indicates a server fault.
	Get(ctx context.Context, key Key) (io.ReadCloser, error)
}
