-- name: CreateUserAccount :one
INSERT INTO accounts (id, created_at, updated_at, account_type)
VALUES ( gen_random_uuid(), NOW(), NOW(), 'user')
RETURNING *;

-- name: CreateServiceIdentityAccount :one
INSERT INTO accounts (id, created_at, updated_at, account_type)
VALUES ( gen_random_uuid(), NOW(), NOW(), 'service_identity')
RETURNING *;

-- service_identities can't be owners or admins and are therefore always treated as members.
-- name: GetAccountByID :one
SELECT 
    a.id ,
    a.account_type, 
    COALESCE(u.user_role, 'member') AS account_role
FROM 
    accounts a
LEFT OUTER JOIN users u
ON a.id = u.account_id
WHERE a.id = $1;
