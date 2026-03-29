-- +goose Up

-- -------------------------------------------------------------------------
-- Account management
-- -------------------------------------------------------------------------

-- accounts: parent table for both users and service accounts
CREATE TABLE accounts (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_type TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE NOT NULL,
    CONSTRAINT account_type_check CHECK (account_type IN ('service_account', 'user'))
);

-- users: human users of the system
-- the first user registered is automatically granted the site-admin role
CREATE TABLE users (
    account_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    email TEXT NOT NULL,
    hashed_password TEXT NOT NULL,
    user_role TEXT NOT NULL,
    CONSTRAINT user_role_check CHECK (user_role IN ('owner', 'admin', 'member')),
    CONSTRAINT fk_user_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

-- case-insensitive unique index on email
CREATE UNIQUE INDEX users_email_unique_ci ON users (lower(email));

-- service_accounts: machine-to-machine accounts
CREATE TABLE service_accounts (
    account_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    client_id TEXT NOT NULL UNIQUE,
    client_contact_email TEXT NOT NULL,
    client_organization TEXT NOT NULL,
    CONSTRAINT fk_service_account_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

-- case-insensitive unique index on service account contact details
CREATE UNIQUE INDEX service_accounts_email_org_unique_ci ON service_accounts (lower(client_contact_email), lower(client_organization));

-- client_secrets: hashed secrets for service account authentication
CREATE TABLE client_secrets (
    hashed_secret TEXT PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    service_account_account_id UUID NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_client_secret_service_account FOREIGN KEY (service_account_account_id) REFERENCES service_accounts(account_id) ON DELETE CASCADE
);

-- one_time_client_secrets: plaintext secrets shown once at service account creation
CREATE TABLE one_time_client_secrets (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    service_account_account_id UUID NOT NULL,
    plaintext_secret TEXT NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT fk_one_time_secret_service_account FOREIGN KEY (service_account_account_id) REFERENCES service_accounts(account_id) ON DELETE CASCADE
);

-- refresh_tokens: long-lived tokens used to obtain new access tokens
CREATE TABLE refresh_tokens (
    hashed_token TEXT PRIMARY KEY,
    user_account_id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_referesh_token_user FOREIGN KEY (user_account_id) REFERENCES users(account_id) ON DELETE CASCADE
);

CREATE INDEX idx_refresh_tokens_user_active ON refresh_tokens (user_account_id, revoked_at, expires_at);

-- password_reset_tokens: time-limited tokens for admin-initiated password resets
CREATE TABLE password_reset_tokens (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_account_id UUID NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_by_admin_id UUID NOT NULL,
    CONSTRAINT fk_password_reset_user FOREIGN KEY (user_account_id) REFERENCES users(account_id) ON DELETE CASCADE,
    CONSTRAINT fk_password_reset_admin FOREIGN KEY (created_by_admin_id) REFERENCES users(account_id) ON DELETE CASCADE
);

CREATE INDEX idx_password_reset_tokens_user ON password_reset_tokens (user_account_id, expires_at);
CREATE INDEX idx_password_reset_tokens_admin ON password_reset_tokens (created_by_admin_id, created_at);
CREATE INDEX idx_password_reset_tokens_expires_at ON password_reset_tokens (expires_at);

-- -------------------------------------------------------------------------
-- Signal types
-- -------------------------------------------------------------------------

-- signal_types: globally defined signal schemas (managed by site-admin)
CREATE TABLE signal_types (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    slug TEXT NOT NULL,
    schema_url TEXT NOT NULL,
    readme_url TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    sem_ver TEXT NOT NULL,
    schema_content TEXT DEFAULT '{}' NOT NULL,
    CONSTRAINT valid_signal_types_slug_format CHECK (slug ~ '^[a-z0-9-]+$'),
    CONSTRAINT valid_schma_json_url CHECK (schema_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.json$'),
    CONSTRAINT valid_readme_url CHECK (readme_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.md$'),
    CONSTRAINT unique_signal_types UNIQUE (slug, sem_ver),
    CONSTRAINT unique_slug_schema_url UNIQUE (slug, schema_url)
);

-- -------------------------------------------------------------------------
-- Information sharing networks (ISNs)
-- -------------------------------------------------------------------------

-- isn: information sharing networks
CREATE TABLE isn (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_account_id UUID NOT NULL,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    detail TEXT NOT NULL,
    is_in_use BOOLEAN DEFAULT TRUE NOT NULL,
    visibility TEXT NOT NULL,
    CONSTRAINT valid_isn_slug_format CHECK (slug ~ '^[a-z0-9-]+$'),
    CONSTRAINT visibility_check CHECK (visibility IN ('public', 'private')),
    CONSTRAINT unique_isn_slug UNIQUE (slug),
    CONSTRAINT fk_isn_user FOREIGN KEY (user_account_id) REFERENCES users(account_id) ON DELETE CASCADE
);

-- isn_accounts: grants read/write access to an ISN for a given account
CREATE TABLE isn_accounts (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    isn_id UUID NOT NULL,
    account_id UUID NOT NULL,
    can_read BOOLEAN DEFAULT FALSE NOT NULL,
    can_write BOOLEAN DEFAULT FALSE NOT NULL,
    CONSTRAINT isn_accounts_unique UNIQUE (isn_id, account_id),
    CONSTRAINT fk_isn_accounts_isn FOREIGN KEY (isn_id) REFERENCES isn(id) ON DELETE CASCADE,
    CONSTRAINT fk_isn_accounts_accounts FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

-- isn_signal_types: associates signal types with ISNs
CREATE TABLE isn_signal_types (
    id BIGINT PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
    isn_id UUID NOT NULL,
    signal_type_id UUID NOT NULL,
    is_in_use BOOLEAN DEFAULT TRUE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT unique_isn_signal_type_association UNIQUE (isn_id, signal_type_id),
    CONSTRAINT fk_isn_signal_types_isn FOREIGN KEY (isn_id) REFERENCES isn(id) ON DELETE CASCADE,
    CONSTRAINT fk_isn_signal_types_signal_type FOREIGN KEY (signal_type_id) REFERENCES signal_types(id) ON DELETE CASCADE
);

-- -------------------------------------------------------------------------
-- Signal processing
-- -------------------------------------------------------------------------

-- signal_batches: groups signals submitted together in a single operation
CREATE TABLE signal_batches (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    isn_id UUID NOT NULL,
    account_id UUID NOT NULL,
    is_latest BOOLEAN DEFAULT TRUE NOT NULL,
    CONSTRAINT unique_signal_batches_id_account_id UNIQUE (id, account_id),
    CONSTRAINT fk_signal_batches_isn FOREIGN KEY (isn_id) REFERENCES isn(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_batches_accounts FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

-- one active batch per account per ISN
CREATE UNIQUE INDEX one_latest_signal_batch_per_account_per_isn_idx ON signal_batches (account_id, isn_id) WHERE (is_latest = true);

-- signal_processing_failures: records signals that failed validation or processing
CREATE TABLE signal_processing_failures (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT now() NOT NULL,
    signal_batch_id UUID NOT NULL,
    signal_type_slug TEXT NOT NULL,
    signal_type_sem_ver TEXT NOT NULL,
    local_ref TEXT NOT NULL,
    error_code TEXT NOT NULL,
    error_message TEXT NOT NULL,
    CONSTRAINT fk_signal_processing_failures_batch FOREIGN KEY (signal_batch_id) REFERENCES signal_batches(id) ON DELETE CASCADE
);

CREATE INDEX idx_signal_processing_failures_batch_id ON signal_processing_failures (signal_batch_id);
CREATE INDEX idx_signal_processing_failures_batch_local_ref ON signal_processing_failures (signal_batch_id, local_ref);
CREATE INDEX idx_signal_processing_failures_error_code ON signal_processing_failures (error_code);

-- -------------------------------------------------------------------------
-- Signal routing
-- -------------------------------------------------------------------------

-- signal_routing_rules: defines which field in a signal type drives routing decisions
CREATE TABLE signal_routing_rules (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    signal_type_id UUID NOT NULL,
    routing_field TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT unique_routing_rule_per_signal_field UNIQUE (signal_type_id, routing_field),
    CONSTRAINT fk_signal_routing_rules_signal_type FOREIGN KEY (signal_type_id) REFERENCES signal_types(id) ON DELETE CASCADE
);

-- signal_routing_mappings: maps field values to destination ISNs
CREATE TABLE signal_routing_mappings (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    signal_routing_rule_id UUID NOT NULL,
    match_pattern TEXT NOT NULL,
    notes TEXT NOT NULL,
    isn_id UUID NOT NULL,
    rule_sequence INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT unique_sequence_per_rule UNIQUE (signal_routing_rule_id, rule_sequence),
    CONSTRAINT fk_signal_routing_mappings_rule FOREIGN KEY (signal_routing_rule_id) REFERENCES signal_routing_rules(id) ON DELETE CASCADE,
    CONSTRAINT fk_signal_routing_mappings_isn FOREIGN KEY (isn_id) REFERENCES isn(id) ON DELETE CASCADE
);

CREATE INDEX idx_signal_routing_mappings_rule_sequence ON signal_routing_mappings (signal_routing_rule_id, rule_sequence);

-- -------------------------------------------------------------------------
-- Signals (hash-partitioned by account_id, 8 partitions)
-- -------------------------------------------------------------------------

-- signals: partitioned by account_id hash (8 partitions)
CREATE TABLE signals (
    id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_id UUID NOT NULL,
    isn_id UUID NOT NULL,
    signal_type_id UUID NOT NULL,
    local_ref TEXT NOT NULL,
    correlation_id UUID NOT NULL,
    is_withdrawn BOOLEAN DEFAULT FALSE NOT NULL,
    is_archived BOOLEAN DEFAULT FALSE NOT NULL,
    CONSTRAINT signals_pkey PRIMARY KEY (id, account_id),
    CONSTRAINT unique_signals_id_account_id UNIQUE (id, account_id),
    CONSTRAINT unique_signals_isn_id_signal_id UNIQUE (isn_id, id, account_id),
    CONSTRAINT unique_signals_account_signal_type_local_ref UNIQUE (account_id, signal_type_id, local_ref)
) PARTITION BY HASH (account_id);

CREATE INDEX idx_signals_isn_signal_type ON ONLY signals (isn_id, signal_type_id);
CREATE INDEX idx_signals_created_at ON ONLY signals (created_at);
CREATE INDEX idx_signals_local_ref ON ONLY signals (local_ref);
CREATE INDEX idx_signals_correlation_id ON ONLY signals (correlation_id);

CREATE TABLE signals_p0 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 0);
CREATE TABLE signals_p1 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 1);
CREATE TABLE signals_p2 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 2);
CREATE TABLE signals_p3 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 3);
CREATE TABLE signals_p4 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 4);
CREATE TABLE signals_p5 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 5);
CREATE TABLE signals_p6 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 6);
CREATE TABLE signals_p7 PARTITION OF signals FOR VALUES WITH (modulus 8, remainder 7);

-- signal_versions: versioned content for signals, partitioned by account_id hash (8 partitions)
CREATE TABLE signal_versions (
    id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_id UUID NOT NULL,
    signal_batch_id UUID NOT NULL,
    signal_id UUID NOT NULL,
    version_number INTEGER NOT NULL,
    content JSONB NOT NULL,
    CONSTRAINT signal_versions_pkey PRIMARY KEY (id, account_id),
    CONSTRAINT unique_signal_id_version_number UNIQUE (account_id, signal_id, version_number),
    CONSTRAINT unique_signal_versions_signal_id_version UNIQUE (signal_id, version_number, account_id)
) PARTITION BY HASH (account_id);

CREATE INDEX idx_signal_versions_signal_id ON ONLY signal_versions (signal_id);
CREATE INDEX idx_signal_versions_signal_id_version ON ONLY signal_versions (signal_id, version_number);
CREATE INDEX idx_signal_versions_created_at ON ONLY signal_versions (created_at);
CREATE INDEX idx_signal_versions_signal_batch_id ON ONLY signal_versions (signal_batch_id);

CREATE TABLE signal_versions_p0 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 0);
CREATE TABLE signal_versions_p1 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 1);
CREATE TABLE signal_versions_p2 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 2);
CREATE TABLE signal_versions_p3 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 3);
CREATE TABLE signal_versions_p4 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 4);
CREATE TABLE signal_versions_p5 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 5);
CREATE TABLE signal_versions_p6 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 6);
CREATE TABLE signal_versions_p7 PARTITION OF signal_versions FOR VALUES WITH (modulus 8, remainder 7);

-- foreign keys for partitioned tables must be added after partition creation
ALTER TABLE signals ADD CONSTRAINT fk_signal_accounts FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE signals ADD CONSTRAINT fk_signal_isn FOREIGN KEY (isn_id) REFERENCES isn(id) ON DELETE CASCADE;
ALTER TABLE signals ADD CONSTRAINT fk_signal_signal_type FOREIGN KEY (signal_type_id) REFERENCES signal_types(id) ON DELETE CASCADE;

ALTER TABLE signal_versions ADD CONSTRAINT fk_signal_version_signal_id FOREIGN KEY (signal_id, account_id) REFERENCES signals(id, account_id) ON DELETE CASCADE;
ALTER TABLE signal_versions ADD CONSTRAINT fk_signal_version_signal_batch FOREIGN KEY (signal_batch_id, account_id) REFERENCES signal_batches(id, account_id) ON DELETE CASCADE;

-- -------------------------------------------------------------------------
-- Views
-- -------------------------------------------------------------------------

-- view: latest version of each signal
CREATE VIEW latest_signal_versions AS
    SELECT id, created_at, account_id, signal_batch_id, signal_id, version_number, content
    FROM signal_versions sv
    WHERE version_number = (
        SELECT max(sv2.version_number)
        FROM signal_versions sv2
        WHERE sv2.signal_id = sv.signal_id
    );

-- +goose Down

DROP VIEW IF EXISTS latest_signal_versions;

DROP TABLE IF EXISTS signal_versions CASCADE;
DROP TABLE IF EXISTS signals CASCADE;
DROP TABLE IF EXISTS signal_routing_mappings CASCADE;
DROP TABLE IF EXISTS signal_routing_rules CASCADE;
DROP TABLE IF EXISTS signal_processing_failures CASCADE;
DROP TABLE IF EXISTS signal_batches CASCADE;
DROP TABLE IF EXISTS isn_signal_types CASCADE;
DROP TABLE IF EXISTS isn_accounts CASCADE;
DROP TABLE IF EXISTS isn CASCADE;
DROP TABLE IF EXISTS signal_types CASCADE;
DROP TABLE IF EXISTS password_reset_tokens CASCADE;
DROP TABLE IF EXISTS refresh_tokens CASCADE;
DROP TABLE IF EXISTS one_time_client_secrets CASCADE;
DROP TABLE IF EXISTS client_secrets CASCADE;
DROP TABLE IF EXISTS service_accounts CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS accounts CASCADE;

