-- +goose Up 
ALTER TABLE signal_types 
DROP COLUMN stage;

ALTER TABLE signal_types 
ADD COLUMN is_in_use BOOL NOT NULL DEFAULT TRUE;

ALTER TABLE signal_types
ADD CONSTRAINT unique_slug_schema_url UNIQUE (slug, schema_url);

-- +goose Down

ALTER TABLE signal_types
ADD COLUMN stage TEXT NOT NULL DEFAULT 'dev';

ALTER TABLE signal_types
  ADD CONSTRAINT stage_check
    CHECK (stage IN ('dev','test', 'live', 'deprecated', 'closed','shuttered'));

ALTER TABLE signal_types 
DROP COLUMN is_in_use;

ALTER TABLE signal_types 
DROP COLUMN is_latest;

ALTER TABLE signal_types
DROP CONSTRAINT unique_slug_schema_url;