-- note: don't display emails on public apis ("GetForDisplay*").

-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES ( gen_random_uuid(), NOW(), NOW(), $1, $2)
RETURNING *;

-- name: UpdatePassword :execrows
UPDATE users SET (updated_at, hashed_password) = (NOW(), $2)
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUsers :many
SELECT u.id, u.email, u.created_at , u.updated_at FROM users u;

-- name: GetUserByID :one
SELECT  u.id, u.email, u.created_at  FROM users u WHERE u.id = $1;

-- name: GetForDisplayUserBySignalDefID :one
SELECT u.id, u.created_at , u.updated_at 
FROM users u 
JOIN signal_defs sd ON u.id = sd.user_id 
WHERE sd.id = $1;

-- name: GetForDisplayUserByIsnID :one
SELECT u.id, u.created_at , u.updated_at 
FROM users u 
JOIN isn i ON u.id = i.user_id 
WHERE i.id = $1;

-- name: ExistsUserWithEmail :one 
SELECT EXISTS (
    SELECT 1 FROM users WHERE email = $1
) AS exists;