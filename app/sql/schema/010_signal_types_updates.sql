-- +goose Up 
ALTER TABLE signal_types 
DROP COLUMN stage;

ALTER TABLE signal_types 
ADD COLUMN is_in_use BOOL NOT NULL DEFAULT TRUE;

ALTER TABLE signal_types
ADD CONSTRAINT unique_slug_schema_url UNIQUE (slug, schema_url);

-- fix error on this unique index
DROP INDEX IF EXISTS one_latest_signal_batch_per_account_idx;
CREATE UNIQUE INDEX one_latest_signal_batch_per_account_per_isn_idx
ON signal_batches (account_id, isn_id) WHERE is_latest = TRUE;

-- +goose Down
DROP INDEX IF EXISTS one_latest_signal_batch_per_account_per_isn_idx;

ALTER TABLE signal_types
ADD COLUMN stage TEXT NOT NULL DEFAULT 'dev';

ALTER TABLE signal_types
  ADD CONSTRAINT stage_check
    CHECK (stage IN ('dev','test', 'live', 'deprecated', 'closed','shuttered'));

ALTER TABLE signal_types 
DROP COLUMN is_in_use;

ALTER TABLE signal_types
DROP CONSTRAINT unique_slug_schema_url;