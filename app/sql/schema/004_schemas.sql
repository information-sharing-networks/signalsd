-- +goose Up
UPDATE signal_types 
SET (schema_url, schema_content) = ('https://github.com/skip/validation/main/schema.json', '{}');

ALTER TABLE signal_types ADD COLUMN schema_content TEXT NOT NULL;

-- +goose Down
ALTER TABLE signal_types DROP COLUMN schema_content;
