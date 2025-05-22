-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (token, user_account_id, created_at, updated_at, expires_at)
VALUES ( $1,$2, NOW(), NOW(), $3)
RETURNING token, user_account_id;

-- name: GetRefreshToken :one
SELECT user_account_id, expires_at, revoked_at FROM refresh_tokens where token = $1;

-- name: RevokeRefreshToken :execrows
UPDATE refresh_tokens SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE token = $1;

-- name: RevokeAllRefreshTokensForUser :execrows
UPDATE refresh_tokens SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE user_account_id = $1
AND revoked_at IS NULL;


