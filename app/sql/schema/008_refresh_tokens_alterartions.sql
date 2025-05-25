-- +goose Up 
ALTER TABLE refresh_tokens
RENAME COLUMN token TO hashed_token;
CREATE INDEX idx_refresh_tokens_user_active ON refresh_tokens (user_account_id, revoked_at, expires_at);

-- +goose Down
ALTER TABLE refresh_tokens
RENAME COLUMN hashed_token TO token;
DROP INDEX idx_refresh_tokens_user_active ;