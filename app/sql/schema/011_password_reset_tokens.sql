-- +goose Up

-- Password reset tokens table 
CREATE TABLE password_reset_tokens (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_account_id UUID NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_by_admin_id UUID NOT NULL,
    CONSTRAINT fk_password_reset_user 
        FOREIGN KEY (user_account_id) 
        REFERENCES users(account_id) 
        ON DELETE CASCADE,
    CONSTRAINT fk_password_reset_admin 
        FOREIGN KEY (created_by_admin_id) 
        REFERENCES users(account_id) 
        ON DELETE CASCADE
);

CREATE INDEX idx_password_reset_tokens_expires_at ON password_reset_tokens (expires_at);

CREATE INDEX idx_password_reset_tokens_admin ON password_reset_tokens (created_by_admin_id, created_at);

CREATE INDEX idx_password_reset_tokens_user ON password_reset_tokens (user_account_id, expires_at);

-- +goose Down

DROP TABLE IF EXISTS password_reset_tokens CASCADE;
