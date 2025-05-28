-- note: don't display emails on public apis ("GetForDisplay*").

-- name: CreateUser :one
INSERT INTO users (account_id, created_at, updated_at, email, hashed_password)
VALUES ( $1, NOW(), NOW(), $2, $3)
RETURNING *;

-- name: CreateOwnerUser :one
INSERT INTO users (account_id, created_at, updated_at, email, hashed_password, user_role)
VALUES ( $1, NOW(), NOW(), $2, $3, 'owner')
RETURNING *;

-- name: UpdatePassword :execrows
UPDATE users SET (updated_at, hashed_password) = (NOW(), $2)
WHERE account_id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUsers :many
SELECT u.account_id, u.email, u.user_role, u.created_at , u.updated_at FROM users u;

-- name: GetUserByID :one
SELECT  u.account_id, u.email, u.user_role, u.created_at  FROM users u WHERE u.account_id = $1;

-- name: GetForDisplayUserBySignalDefID :one
SELECT u.account_id, u.created_at , u.updated_at 
FROM users u 
JOIN signal_types sd ON u.account_id = sd.user_account_id 
WHERE sd.id = $1;

-- name: GetForDisplayUserByIsnID :one
SELECT u.account_id, u.created_at , u.updated_at 
FROM users u 
JOIN isn i ON u.account_id = i.user_account_id 
WHERE i.id = $1;

-- name: UpdateUserAccountToAdmin :execrows 
UPDATE users 
SET 
    user_role = 'admin'
WHERE 
    account_id = $1;


-- name: UpdateUserAccountToMember :execrows 
UPDATE users 
SET 
    user_role = 'member'
WHERE 
    account_id = $1;

-- name: ExistsUserWithEmail :one 
SELECT EXISTS (
    SELECT 1 FROM users WHERE email = $1
) AS exists;

-- name: IsFirstUser :one
SELECT COUNT(*) = 0 AS is_empty
FROM users;