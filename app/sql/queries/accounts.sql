-- name: CreateUserAccount :one
INSERT INTO accounts (id, created_at, updated_at, account_type, is_active)
VALUES ( gen_random_uuid(), NOW(), NOW(), 'user', true)
RETURNING *;

-- name: CreateServiceAccountAccount :one
INSERT INTO accounts (id, created_at, updated_at, account_type, is_active)
VALUES ( gen_random_uuid(), NOW(), NOW(), 'service_account', true)
RETURNING *;

-- return the account and user_role (user_role is not applicable to service_accounts - which are always treated as members - so just return 'member' in these cases)
-- returns both active and inactive accounts
-- name: GetAccountByID :one
SELECT
    a.id ,
    a.account_type,
    a.is_active,
    COALESCE(u.user_role, 'member') AS account_role
FROM
    accounts a
LEFT OUTER JOIN users u
ON a.id = u.account_id
WHERE a.id = $1;

-- name: DisableAccount :execrows
UPDATE accounts SET (updated_at, is_active) = (NOW(), false)
WHERE id = $1;

-- name: EnableAccount :execrows
UPDATE accounts SET (updated_at, is_active) = (NOW(), true)
WHERE id = $1;