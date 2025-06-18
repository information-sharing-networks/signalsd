-- +goose Up

-- Drop existing tables and recreate with account-based hash partitioning

DROP TABLE IF EXISTS signal_versions CASCADE;
DROP TABLE IF EXISTS signals CASCADE;

ALTER TABLE signal_batches ADD CONSTRAINT unique_signal_batches_id_account_id UNIQUE (id, account_id);

CREATE TABLE signals (
    id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_id UUID NOT NULL,
    isn_id UUID NOT NULL,
    signal_type_id UUID NOT NULL,
    local_ref TEXT NOT NULL,
    correlation_id UUID NOT NULL,
    is_withdrawn BOOL NOT NULL DEFAULT false,
    is_archived BOOL NOT NULL DEFAULT false,
    CONSTRAINT fk_signal_accounts FOREIGN KEY (account_id)
        REFERENCES accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_isn FOREIGN KEY (isn_id)
        REFERENCES isn(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_signal_type FOREIGN KEY (signal_type_id)
        REFERENCES signal_types(id) ON DELETE CASCADE,
    CONSTRAINT unique_signals_id_account_id UNIQUE (id, account_id)
) PARTITION BY HASH (account_id);

CREATE TABLE signal_versions (
    id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_id UUID NOT NULL,
    signal_batch_id UUID NOT NULL,
    signal_id UUID NOT NULL,
    version_number INT NOT NULL,
    content JSONB NOT NULL,
    CONSTRAINT unique_signal_id_version_number UNIQUE (account_id, signal_id, version_number),
    CONSTRAINT fk_signal_version_signal_id FOREIGN KEY (signal_id,account_id)
        REFERENCES signals(id,account_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_signal_version_signal_batch FOREIGN KEY (signal_batch_id, account_id)
        REFERENCES signal_batches(id, account_id) ON DELETE CASCADE
) PARTITION BY HASH (account_id);

CREATE TABLE signals_p0 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 0);
CREATE TABLE signals_p1 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 1);
CREATE TABLE signals_p2 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 2);
CREATE TABLE signals_p3 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 3);
CREATE TABLE signals_p4 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 4);
CREATE TABLE signals_p5 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 5);
CREATE TABLE signals_p6 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 6);
CREATE TABLE signals_p7 PARTITION OF signals FOR VALUES WITH (MODULUS 8, REMAINDER 7);

CREATE TABLE signal_versions_p0 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 0);
CREATE TABLE signal_versions_p1 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 1);
CREATE TABLE signal_versions_p2 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 2);
CREATE TABLE signal_versions_p3 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 3);
CREATE TABLE signal_versions_p4 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 4);
CREATE TABLE signal_versions_p5 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 5);
CREATE TABLE signal_versions_p6 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 6);
CREATE TABLE signal_versions_p7 PARTITION OF signal_versions FOR VALUES WITH (MODULUS 8, REMAINDER 7);

ALTER TABLE signals ADD CONSTRAINT signals_pkey PRIMARY KEY (id, account_id);
ALTER TABLE signal_versions ADD CONSTRAINT signal_versions_pkey PRIMARY KEY (id, account_id);

-- Unique constraints (must include partition key)
ALTER TABLE signals ADD CONSTRAINT unique_signals_account_signal_type_local_ref
    UNIQUE (account_id, signal_type_id, local_ref);
ALTER TABLE signals ADD CONSTRAINT unique_signals_isn_id_signal_id
    UNIQUE (isn_id, id, account_id);

ALTER TABLE signal_versions ADD CONSTRAINT unique_signal_versions_signal_id_version
    UNIQUE (signal_id, version_number, account_id);

CREATE INDEX idx_signals_created_at ON signals (created_at);
CREATE INDEX idx_signals_isn_signal_type ON signals (isn_id,signal_type_id);
CREATE INDEX idx_signals_correlation_id ON signals (correlation_id);
CREATE INDEX idx_signals_local_ref ON signals (local_ref);

CREATE INDEX idx_signal_versions_signal_id_version ON signal_versions (signal_id, version_number);
CREATE INDEX idx_signal_versions_signal_id ON signal_versions (signal_id);
CREATE INDEX idx_signal_versions_created_at ON signal_versions (created_at);
CREATE INDEX idx_signal_versions_signal_batch_id ON signal_versions (signal_batch_id);

-- +goose Down

DROP TABLE IF EXISTS signal_versions CASCADE;
DROP TABLE IF EXISTS signals CASCADE;

CREATE TABLE signals (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_id UUID NOT NULL,
    isn_id UUID NOT NULL,
    signal_type_id UUID NOT NULL,
    local_ref TEXT NOT NULL,
    correlation_id UUID NOT NULL,
    is_withdrawn BOOL NOT NULL DEFAULT false,
    is_archived BOOL NOT NULL DEFAULT false,
    CONSTRAINT unique_signals_account_id_signal_type_local_ref UNIQUE (account_id, signal_type_id, local_ref),
    CONSTRAINT unique_signals_isn_id_signal_id UNIQUE (isn_id, id),
    CONSTRAINT fk_correlation_id FOREIGN KEY (isn_id,correlation_id)
        REFERENCES signals(isn_id,id),
    CONSTRAINT fk_signal_accounts FOREIGN KEY (account_id)
        REFERENCES accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_isn FOREIGN KEY (isn_id)
        REFERENCES isn(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_signal_type FOREIGN KEY (signal_type_id)
        REFERENCES signal_types(id) ON DELETE CASCADE
);

CREATE TABLE signal_versions (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_id UUID NOT NULL,
    signal_batch_id UUID NOT NULL,
    signal_id UUID NOT NULL,
    version_number INT NOT NULL,
    validation_status TEXT NOT NULL DEFAULT 'pending',
    content JSONB NOT NULL,
    CONSTRAINT unique_signal_id_version_number UNIQUE (signal_id, version_number),
    CONSTRAINT fk_signal_version_signal_id FOREIGN KEY (signal_id,account_id)
        REFERENCES signals(id, account_id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_version_signal_batch FOREIGN KEY (signal_batch_id,account_id)
        REFERENCES signal_batches(id, account_id)
        ON DELETE CASCADE,
    CONSTRAINT validation_status_check CHECK (validation_status IN ('pending','valid', 'invalid', 'n/a'))
);

ALTER TABLE signal_batches DROP CONSTRAINT unique_signal_batches_id_account_id;