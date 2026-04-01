-- name: CreateUser :one
INSERT INTO users (account_id, created_at, updated_at, email, hashed_password, user_role)
VALUES ( $1, NOW(), NOW(), $2, $3, 'member')
RETURNING *;

-- name: CreateSiteAdminUser :one
-- first user is always a site admin
INSERT INTO users (account_id, created_at, updated_at, email, hashed_password, user_role)
VALUES ( $1, NOW(), NOW(), $2, $3, 'siteadmin')
RETURNING *;

-- name: UpdatePassword :execrows
UPDATE users SET (updated_at, hashed_password) = (NOW(), $2)
WHERE account_id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE LOWER(email) = LOWER(sqlc.arg(email));

-- name: GetUsers :many
SELECT u.account_id, u.email, u.user_role, u.created_at , u.updated_at FROM users u;

-- name: GetUserByID :one
SELECT  u.account_id, u.email, u.user_role, u.created_at , u.updated_at FROM users u WHERE u.account_id = $1;

-- name: GetUserWithPasswordByID :one
SELECT u.account_id, u.email, u.hashed_password FROM users u WHERE u.account_id = $1;

-- name: GetUserByIsnID :one
SELECT u.*
FROM users u 
JOIN isn i ON u.account_id = i.user_account_id 
WHERE i.id = $1;

-- name: UpdateUserAccountToIsnAdmin :execrows 
UPDATE users 
SET 
    user_role = 'isnadmin'
WHERE 
    account_id = $1;


-- name: UpdateUserAccountToMember :execrows
UPDATE users
SET
    user_role = 'member'
WHERE
    account_id = $1;

-- name: UpdateUserAccountToSiteAdmin :execrows
UPDATE users
SET
    user_role = 'siteadmin'
WHERE
    account_id = $1;

-- name: ExistsUserWithEmail :one
SELECT exists (
    SELECT 1 FROM users WHERE LOWER(email) = LOWER(sqlc.arg(email))
) AS exists;

-- name: IsFirstUser :one
SELECT COUNT(*) = 0 AS is_empty
FROM users;


-- name: GetEmailByAccountID :one
SELECT COALESCE(u.email, sa.client_contact_email) AS email 
FROM accounts a
LEFT OUTER JOIN service_accounts sa
    ON sa.account_id = a.id
LEFT OUTER JOIN users u
    ON u.account_id = a.id
WHERE a.id = $1;