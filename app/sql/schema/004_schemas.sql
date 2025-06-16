-- +goose Up

-- mark existing signal_types as 'skip validation'
ALTER TABLE signal_types ADD COLUMN schema_content TEXT NOT NULL DEFAULT '{}';


-- sadly this won't work in every case
--UPDATE signal_types 
--SET (schema_url, schema_content) = ('https://github.com/skip/validation/main/schema.json', '{}');

-- so...
DELETE FROM signal_types CASCADE;

-- to keep things simple, JSON schema validation behaviour is now decided when setting up the signal type - if validation is used it is done at the point signals are created.
ALTER TABLE signal_versions DROP CONSTRAINT validation_status_check;

-- +goose Down
ALTER TABLE signal_types DROP COLUMN schema_content;

ALTER TABLE signal_versions ADD CONSTRAINT validation_status_check CHECK (validation_status IN ('pending','valid', 'invalid', 'n/a'));
