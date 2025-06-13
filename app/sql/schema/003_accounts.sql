-- +goose Up
-- add missing constraint on isn
ALTER TABLE isn ADD CONSTRAINT valid_isn_slug_format
        CHECK (slug ~ '^[a-z0-9-]+$');

ALTER TABLE service_accounts DROP COLUMN is_active;
ALTER TABLE accounts ADD COLUMN is_active BOOL NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE isn DROP CONSTRAINT valid_isn_slug_format;
ALTER TABLE service_accounts ADD COLUMN is_active BOOL NOT NULL DEFAULT true;
ALTER TABLE accounts DROP COLUMN is_active;