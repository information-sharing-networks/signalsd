-- +goose Up 

CREATE TABLE signal_batches (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE,
    isn_id UUID NOT NULL,
    account_id UUID NOT NULL,
    is_latest BOOL NOT NULL DEFAULT TRUE,
    account_type TEXT NOT NULL,
CONSTRAINT signal_batches_account_type_check
    CHECK (account_type IN ('service_identity','user')),
CONSTRAINT fk_signal_batches_isn FOREIGN KEY (isn_id)
    REFERENCES isn(id)
    ON DELETE CASCADE,
CONSTRAINT fk_signal_batches_accounts FOREIGN KEY (account_id)
    REFERENCES accounts(id)
    ON DELETE CASCADE
);
CREATE UNIQUE INDEX one_latest_signal_batch_per_account_idx
ON signal_batches (account_id) WHERE is_latest = TRUE;

-- Records table
CREATE TABLE signals (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE,
    account_id UUID NOT NULL,
    signal_batch_id UUID NOT NULL,
    correlation_id UUID NOT NULL,
    signal_type_id UUID NOT NULL,
    local_ref TEXT NOT NULL DEFAULT 'pending',
    version_number INT NOT NULL,
    is_latest BOOL NOT NULL DEFAULT true,
    is_withdrawn BOOL NOT NULL DEFAULT false,
    is_archived BOOL NOT NULL DEFAULT false,
    validation_status TEXT NOT NULL,
    json_payload JSONB NOT NULL, 
CONSTRAINT unique_local_ref_version UNIQUE (account_id, signal_type_id, local_ref, version_number),
CONSTRAINT validation_status_check CHECK (validation_status IN ('valid', 'invalid', 'n/a')),
CONSTRAINT fk_signal_signal_batch FOREIGN KEY (signal_batch_id)
    REFERENCES signal_batches(id)
    ON DELETE CASCADE,
CONSTRAINT fk_signal_signal_type FOREIGN KEY (signal_type_id)
    REFERENCES signal_types(id)
    ON DELETE CASCADE,
CONSTRAINT fk_correlation_id FOREIGN KEY (correlation_id)
    REFERENCES signals(id),
CONSTRAINT fk_signal_accounts FOREIGN KEY (account_id)
    REFERENCES accounts(id)
    ON DELETE CASCADE
);
CREATE UNIQUE INDEX one_latest_signal_per_local_ref_idx
ON signals (account_id, signal_type_id, local_ref, version_number) WHERE is_latest = TRUE;


CREATE TABLE isn_accounts (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE,
    isn_id UUID NOT NULL,
    account_id UUID NOT NULL,
    permission TEXT NOT NULL,
CONSTRAINT isn_accounts_unique UNIQUE (isn_id, account_id ),
CONSTRAINT isn_accounts_permission_check
    CHECK (permission IN ('read','write')),
CONSTRAINT fk_isn_accounts_accounts FOREIGN KEY (account_id)
    REFERENCES accounts(id)
    ON DELETE CASCADE,
CONSTRAINT fk_isn_accounts_isn FOREIGN KEY (isn_id)
    REFERENCES isn(id)
    ON DELETE CASCADE 
);

-- +goose Down

DROP TABLE IF EXISTS signals CASCADE ;
DROP TABLE IF EXISTS signal_batches CASCADE;
DROP TABLE IF EXISTS isn_accounts;
