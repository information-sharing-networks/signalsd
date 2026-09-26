// package documents stores the content of document signals (e.g. pdfs and other binary formats).
//
// The document metadata (name, mime type, size and sha256) is stored as the content of the signal version.
// This package stores the document bytes, which are held separately.
//
// Documents are content-addressed within an account: the key is the account ID plus the sha256 of the content.
//
// The account ID is included so that every stored document has
// exactly one owner and we avoid issues where two accounts load the same document and then one wants to delete it.
//
// Storing the same content again is a no-op, so uploads can be safely retried.
//
// Store is the interface to the storage backend. Postgres is the only backend for now (S3 will be added later),
// selected with the DOCUMENT_STORE setting.
//
// Streaming: the Store interface takes and returns readers so that callers never need to hold a whole document.
// Postgres can't stream BYTEA values, so the postgres backend reads each document into memory internally
// (up to MAX_DOCUMENT_SIZE per concurrent upload or download).
// An S3 backend is planned and will stream the storage - this should be used when handling large docs or lots of
// concurrent loads.
package documents
