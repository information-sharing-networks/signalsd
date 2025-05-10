-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES ( gen_random_uuid(), NOW(), NOW(), $1, $2)
RETURNING *;

-- name: UpdateUserEmailAndPassword :execrows
UPDATE users SET (updated_at, email, hashed_password) = (NOW(), $2, $3)
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;


-- name: GetAPIUsers :many
SELECT u.id, u.email, u.created_at , u.updated_at FROM users u;

-- name: GetAPIUserByID :one
SELECT  u.id, u.email, u.created_at  FROM users u WHERE u.id = $1;

-- name: GetAPIUserBySignalDefID :one
SELECT u.id, u.email, u.created_at , u.updated_at 
FROM users u 
JOIN signal_defs sd ON u.id = sd.user_id 
WHERE sd.id = $1;

-- name: ExistsUserWithEmail :one 
SELECT EXISTS (
    SELECT 1 FROM users WHERE email = $1
) AS exists;