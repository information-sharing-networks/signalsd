-- +goose Up 

CREATE TABLE signal_batches (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
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
-- this table is the master signal record rerpresenting a unique account/signal type/local ref combination
-- the only updateable field is correlation_id.
CREATE TABLE signals (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_id UUID NOT NULL,
    signal_type_id UUID NOT NULL,
    local_ref TEXT NOT NULL,
    correlation_id UUID NOT NULL,
    is_withdrawn BOOL NOT NULL DEFAULT false,
    is_archived BOOL NOT NULL DEFAULT false,
CONSTRAINT unique_signals_account_id_signal_type_local_ref UNIQUE (account_id, signal_type_id, local_ref),
CONSTRAINT fk_signal_signal_type FOREIGN KEY (signal_type_id)
    REFERENCES signal_types(id)
    ON DELETE CASCADE,
-- these constaints prevent users accidentally correlating signals of different types
CONSTRAINT unique_signals_signal_type_id_correlation_id UNIQUE (signal_type_id, id),
CONSTRAINT fk_correlation_id FOREIGN KEY (signal_type_id,correlation_id)
    REFERENCES signals(signal_type_id,id),
CONSTRAINT fk_signal_accounts FOREIGN KEY (account_id)
    REFERENCES accounts(id)
    ON DELETE CASCADE
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
    CONSTRAINT fk_signal_version_signal_id FOREIGN KEY (signal_id)
        REFERENCES signals(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_signal_version_signal_batch FOREIGN KEY (signal_batch_id)
        REFERENCES signal_batches(id)
        ON DELETE CASCADE,
    CONSTRAINT validation_status_check CHECK (validation_status IN ('pending','valid', 'invalid', 'n/a'))
);


CREATE TABLE isn_accounts (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL ,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
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

DROP TABLE IF EXISTS signal_versions CASCADE;
DROP TABLE IF EXISTS signals CASCADE ;
DROP TABLE IF EXISTS signal_batches CASCADE;
DROP TABLE IF EXISTS isn_accounts CASCADE;
