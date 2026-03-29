-- Revert signal types from ISN-scoped to global for the site
-- this change was made to support the new routing feature.

-- +goose Up
-- link signal types to ISNs
CREATE TABLE isn_signal_types (
    id BIGSERIAL PRIMARY KEY,
    isn_id UUID NOT NULL,
    signal_type_id UUID NOT NULL,
    is_in_use BOOL NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT fk_isn_signal_types_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_isn_signal_types_signal_type
        FOREIGN KEY (signal_type_id)
        REFERENCES signal_types(id)
        ON DELETE CASCADE,
    CONSTRAINT unique_isn_signal_type_association
        UNIQUE (isn_id, signal_type_id)
);

-- note this automatcally drops the fk_signal_types_isn and unique_signal_types constraints
ALTER TABLE signal_types DROP COLUMN isn_id;

ALTER TABLE signal_types ADD CONSTRAINT unique_signal_types UNIQUE (slug, sem_ver);

-- remove the is_in_use column from signal_types (will now be controlled at the ISN level)
ALTER TABLE signal_types DROP COLUMN is_in_use; 

-- +goose Down

ALTER TABLE signal_types ADD COLUMN isn_id UUID NOT NULL;

-- Restore the old unique constraint (scoped to ISN)
ALTER TABLE signal_types DROP CONSTRAINT unique_signal_types;
ALTER TABLE signal_types ADD CONSTRAINT unique_signal_types UNIQUE (isn_id, slug, sem_ver);

-- Restore the foreign key constraint
ALTER TABLE signal_types ADD CONSTRAINT fk_signal_types_isn
    FOREIGN KEY (isn_id)
    REFERENCES isn(id)
    ON DELETE CASCADE;

-- add the is_in_use column back to signal_types (will now be controlled at the global level)
ALTER TABLE signal_types ADD COLUMN is_in_use BOOL NOT NULL DEFAULT true; 

-- Drop the junction table
DROP TABLE isn_signal_types;

