-- +goose Up

-- this will hold the combined list of service accounts and users 
CREATE TABLE accounts (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_type TEXT NOT NULL,
    CONSTRAINT account_type_check
    CHECK (account_type IN ('service_identity','user'))
);

CREATE TABLE users (
    account_id uuid PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    email TEXT NOT NULL,
    hashed_password TEXT NOT NULL,
    user_role text NOT NULL,
    CONSTRAINT user_role_check
        CHECK (user_role IN ('owner','admin','user')),
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT fk_user_account FOREIGN KEY (account_id) 
        REFERENCES accounts(id) 
        ON DELETE CASCADE
);

CREATE TABLE service_accounts (
    account_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    client_id TEXT UNIQUE NOT NULL,
    client_secret TEXT NOT NULL,
    client_contact_email TEXT NOT NULL,
    CONSTRAINT fk_service_account_account
        FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE
);

CREATE TABLE isn (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_account_id UUID NOT NULL,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    detail TEXT NOT NULL,
    is_in_use bool DEFAULT true NOT NULL,
    visibility TEXT NOT NULL,
    CONSTRAINT visibility_check
    CHECK (visibility IN ('public','private')),
    CONSTRAINT unique_isn_slug UNIQUE (slug),
    CONSTRAINT fk_isn_user
        FOREIGN KEY (user_account_id)
        REFERENCES users(account_id)
        ON DELETE CASCADE
);

CREATE TABLE signal_types (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    isn_id UUID NOT NULL,
    slug TEXT NOT NULL,
    schema_url TEXT NOT NULL,
    readme_url TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    sem_ver TEXT NOT NULL,
    is_in_use BOOL NOT NULL,
    CONSTRAINT unique_signal_types UNIQUE (slug, sem_ver),
    CONSTRAINT unique_slug_schema_url UNIQUE (slug, schema_url),
    CONSTRAINT valid_signal_types_slug_format
        CHECK (slug ~ '^[a-z0-9-]+$'),
    CONSTRAINT valid_schma_json_url 
        CHECK (schema_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.json$'),
    CONSTRAINT valid_readme_url 
        CHECK (readme_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.md$'),
    CONSTRAINT fk_signal_types_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE
 );

CREATE TABLE refresh_tokens (
    hashed_token text NOT NULL PRIMARY KEY,
    user_account_id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_referesh_token_user
        FOREIGN KEY (user_account_id)
        REFERENCES users(account_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_refresh_tokens_user_active ON refresh_tokens (user_account_id, revoked_at, expires_at);

CREATE TABLE signal_batches (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    isn_id UUID NOT NULL,
    account_id UUID NOT NULL,
    is_latest BOOL NOT NULL DEFAULT TRUE,
    account_type TEXT NOT NULL,
    CONSTRAINT signal_batches_account_type_check
        CHECK (account_type IN ('service_account','user')),
    CONSTRAINT fk_signal_batches_isn FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_signal_batches_accounts FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE
);
CREATE UNIQUE INDEX one_latest_signal_batch_per_account_per_isn_idx
ON signal_batches (account_id, isn_id) WHERE is_latest = TRUE;

-- signals master table
-- a signal master record represents a unique account/signal type/local ref combination
-- the only updateable field is correlation_id.
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
    REFERENCES accounts(id)
    ON DELETE CASCADE,
CONSTRAINT fk_signal_isn FOREIGN KEY (isn_id) 
    REFERENCES isn(id) 
    ON DELETE CASCADE,
CONSTRAINT fk_signal_signal_type FOREIGN KEY (signal_type_id) 
    REFERENCES signal_types(id)
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
DROP table IF EXISTS signal_types CASCADE;
DROP table IF EXISTS refresh_tokens CASCADE;
DROP table IF EXISTS isn CASCADE;
DROP table IF EXISTS users CASCADE;
DROP table IF EXISTS service_accounts CASCADE;
DROP table IF EXISTS accounts CASCADE;