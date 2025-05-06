-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES ( gen_random_uuid(), NOW(), NOW(), $1, $2)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: ExistsUserWithEmail :one 
SELECT EXISTS (
    SELECT 1 FROM users WHERE email = $1
) AS exists;

-- name: UpdateUserEmailAndPassword :execrows
UPDATE users SET (updated_at, email, hashed_password) = (NOW(), $2, $3)
WHERE id = $1;
