-- +goose Up
DELETE from isn;
ALTER TABLE isn 
ADD COLUMN storage_connection_url TEXT NOT NULL;

-- +goose Down
ALTER TABLE isn 
DROP COLUMN storage_connection_url;

