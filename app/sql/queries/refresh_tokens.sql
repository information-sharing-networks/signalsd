-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (hashed_token, user_account_id, created_at, updated_at, expires_at)
VALUES ( $1,$2, NOW(), NOW(), $3)
RETURNING hashed_token, user_account_id;

-- name: GetValidRefreshTokenByUserAccountId :one
SELECT hashed_token, expires_at
FROM refresh_tokens
WHERE user_account_id = $1
  AND revoked_at IS NULL
  AND expires_at > NOW();


-- name: GetRefreshToken :one
SELECT user_account_id, expires_at, revoked_at FROM refresh_tokens where hashed_token = $1;

-- name: GetValidRefreshTokenByHashedToken :one
SELECT user_account_id, expires_at
FROM refresh_tokens
WHERE hashed_token = $1
  AND revoked_at IS NULL
  AND expires_at > NOW();

-- name: RevokeRefreshToken :execrows
UPDATE refresh_tokens SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE hashed_token = $1;

-- name: RevokeAllRefreshTokensForUser :execrows
UPDATE refresh_tokens SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE user_account_id = $1
AND revoked_at IS NULL;

-- name: CountActiveRefreshTokens :one
-- used as part of integration tests
SELECT COUNT(*) AS unrevoked_count 
FROM refresh_tokens
WHERE user_account_id = $1
AND revoked_at IS NOT NULL;


