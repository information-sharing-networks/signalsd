-- +goose Up

-- Store one-time link tokens as hashes (like refresh tokens and client secrets), so that read access to the
-- database is not enough to use a pending password reset or service account setup link.
--
-- The links contain the token and the server looks up HashToken(token): sha512, base64 url encoded (with padding).
-- Existing tokens are UUIDs, so pending links keep working: the expression below is the same hash of the UUID string.

-- password reset tokens: replace the token id with its hash
ALTER TABLE password_reset_tokens ADD COLUMN hashed_token TEXT;

UPDATE password_reset_tokens
SET hashed_token = translate(replace(encode(sha512(convert_to(id::text, 'UTF8')), 'base64'), E'\n', ''), '+/', '-_');

ALTER TABLE password_reset_tokens ALTER COLUMN hashed_token SET NOT NULL;
ALTER TABLE password_reset_tokens DROP COLUMN id;
ALTER TABLE password_reset_tokens ADD PRIMARY KEY (hashed_token);

-- service account setup tokens replace one_time_client_secrets: the client secret is now generated when the setup link
-- is used, so it is never stored in plain text (only its hash, in client_secrets)
CREATE TABLE service_account_setup_tokens (
    hashed_token TEXT PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    service_account_account_id UUID NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT fk_setup_token_service_account FOREIGN KEY (service_account_account_id) REFERENCES service_accounts(account_id) ON DELETE CASCADE
);

CREATE INDEX idx_service_account_setup_tokens_account ON service_account_setup_tokens (service_account_account_id);

INSERT INTO service_account_setup_tokens (hashed_token, created_at, service_account_account_id, expires_at)
SELECT translate(replace(encode(sha512(convert_to(id::text, 'UTF8')), 'base64'), E'\n', ''), '+/', '-_'),
    created_at, service_account_account_id, expires_at
FROM one_time_client_secrets;

DROP TABLE one_time_client_secrets;

-- +goose Down

-- hashes can't be turned back into tokens, so pending password reset and service account setup links are deleted
-- (they have to be issued again)

DROP TABLE service_account_setup_tokens;

CREATE TABLE one_time_client_secrets (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    service_account_account_id UUID NOT NULL,
    plaintext_secret TEXT NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    CONSTRAINT fk_one_time_secret_service_account FOREIGN KEY (service_account_account_id) REFERENCES service_accounts(account_id) ON DELETE CASCADE
);

DELETE FROM password_reset_tokens;
ALTER TABLE password_reset_tokens DROP COLUMN hashed_token;
ALTER TABLE password_reset_tokens ADD COLUMN id UUID PRIMARY KEY;
