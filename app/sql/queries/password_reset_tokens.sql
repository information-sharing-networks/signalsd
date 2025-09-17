-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (id, user_account_id, created_at, expires_at, created_by_admin_id)
VALUES ($1, $2, NOW(), $3, $4)
RETURNING id;

-- name: GetPasswordResetToken :one
SELECT created_at, user_account_id, expires_at, created_by_admin_id
FROM password_reset_tokens
WHERE id = $1;

-- name: DeletePasswordResetToken :execrows
DELETE FROM password_reset_tokens 
WHERE id = $1;

-- name: DeleteExpiredPasswordResetTokens :execrows
DELETE FROM password_reset_tokens 
WHERE expires_at < NOW();

-- name: DeletePasswordResetTokensForUser :execrows
DELETE FROM password_reset_tokens 
WHERE user_account_id = $1;

-- name: CountActivePasswordResetTokensForUser :one
-- used for rate limiting and testing
SELECT COUNT(*) AS active_tokens 
FROM password_reset_tokens
WHERE user_account_id = $1
AND expires_at > NOW();

-- name: GetPasswordResetTokensCreatedByAdmin :many
-- for admin audit queries
SELECT id, created_at, user_account_id, expires_at
FROM password_reset_tokens
WHERE created_by_admin_id = $1
ORDER BY created_at DESC;
