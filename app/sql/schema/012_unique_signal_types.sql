-- Make signal type uniqueness depend on isn_id

-- +goose Up
ALTER TABLE signal_types DROP CONSTRAINT unique_signal_types;
ALTER TABLE signal_types ADD CONSTRAINT unique_signal_types UNIQUE (isn_id, slug, sem_ver);

-- +goose Down
ALTER TABLE signal_types DROP CONSTRAINT unique_signal_types;
ALTER TABLE signal_types ADD CONSTRAINT unique_signal_types UNIQUE (slug, sem_ver);
