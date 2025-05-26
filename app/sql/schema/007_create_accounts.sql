-- +goose Up 
-- rename the user.id column to user_account_id
DELETE FROM users;

ALTER TABLE users
RENAME COLUMN id TO account_id;

ALTER TABLE isn
RENAME COLUMN user_id TO user_account_id;

ALTER TABLE refresh_tokens
RENAME COLUMN user_id TO user_account_id;

ALTER TABLE users 
ADD COLUMN user_role TEXT DEFAULT 'member' NOT NULL;

ALTER TABLE users 
ADD CONSTRAINT user_role_check
    CHECK (user_role IN ('owner','admin','member'));

-- this will hold the combined list of service accounts and users 
CREATE TABLE accounts (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    account_type TEXT NOT NULL,
    CONSTRAINT account_type_check
    CHECK (account_type IN ('service_identity','user'))
);

ALTER TABLE users
ADD CONSTRAINT fk_user_account
        FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE;


CREATE TABLE service_identities (
    account_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    client_id TEXT UNIQUE NOT NULL,
    client_secret TEXT NOT NULL,
    client_contact_email TEXT NOT NULL,
    CONSTRAINT fk_service_identity_user
        FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS service_identities CASCADE;
DROP TABLE IF EXISTS accounts CASCADE;
ALTER TABLE users
RENAME COLUMN account_id TO id;

ALTER TABLE users 
DROP COLUMN user_role;

ALTER TABLE isn
RENAME COLUMN user_account_id TO user_id;

ALTER TABLE refresh_tokens
RENAME COLUMN user_account_id TO user_id;