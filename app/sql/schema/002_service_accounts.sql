-- +goose Up
DROP TABLE IF EXISTS service_accounts;

ALTER TABLE accounts
      DROP CONSTRAINT account_type_check;
ALTER TABLE accounts
    ADD CONSTRAINT account_type_check
    CHECK (account_type IN ('service_account','user'));

CREATE TABLE service_accounts (
    account_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    client_id TEXT UNIQUE NOT NULL,
    client_contact_email TEXT NOT NULL,
    client_organization TEXT NOT NULL,
    rate_limit_per_minute INT NOT NULL,
    is_active BOOL NOT NULL,
    CONSTRAINT fk_service_account_unique UNIQUE (client_contact_email, client_organization),
    CONSTRAINT fk_service_account_account
        FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE
);


CREATE TABLE client_secrets (
    hashed_secret text NOT NULL PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    service_account_account_id UUID NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_client_secret_service_account
        FOREIGN KEY (service_account_account_id)
        REFERENCES service_accounts(account_id)
        ON DELETE CASCADE
);

CREATE TABLE one_time_client_secrets (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    service_account_account_id UUID NOT NULL,
    plaintext_secret TEXT NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT fk_one_time_secret_service_account
        FOREIGN KEY (service_account_account_id)
        REFERENCES service_accounts(account_id)
        ON DELETE CASCADE
);


-- +goose Down

DROP TABLE IF EXISTS client_secrets;
DROP TABLE IF EXISTS one_time_client_secrets;
DROP TABLE IF EXISTS service_accounts;

CREATE TABLE service_accounts (
    account_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    client_id TEXT UNIQUE NOT NULL,
    client_secret TEXT NOT NULL,
    organization TEXT NOT NULL,
    contact_email TEXT NOT NULL,
    CONSTRAINT fk_service_account_account
        FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE
);
