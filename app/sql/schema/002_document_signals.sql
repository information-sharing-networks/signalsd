-- +goose Up

-- this change introduces the concept of signal *content kinds*
-- The original implemenation only supported JSON, this change adds support for documents.
--
-- Document signals
--
-- A document is a signal with binary content (e.g a pdf)
-- the signal type is the document type (e.g. bill-of-lading/v1.0.0).
-- signal_versions.content holds a server-generated document metadata (name, mime_type, size_bytes, sha256).
-- The file itself is stored separately (document_contents is used when storage is in postgres)
-- -------------------------------------------------------------------------

ALTER TABLE signal_types
    ADD COLUMN content_kind TEXT DEFAULT 'json' NOT NULL,
    ADD CONSTRAINT valid_content_kind CHECK (content_kind IN ('json', 'document'));

-- document_contents: document bytes (used when documents are stored in postgres)
CREATE TABLE document_contents (
    account_id UUID NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    content BYTEA NOT NULL,
    CONSTRAINT document_contents_pkey PRIMARY KEY (account_id, sha256),
    CONSTRAINT valid_sha256 CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    CONSTRAINT fk_document_contents_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

-- +goose Down

DROP TABLE IF EXISTS document_contents;

ALTER TABLE signal_types
    DROP CONSTRAINT IF EXISTS valid_content_kind,
    DROP COLUMN IF EXISTS content_kind;
