-- name: CreateUserAccount :one
INSERT INTO accounts (id, created_at, updated_at, account_type)
VALUES ( gen_random_uuid(), NOW(), NOW(), 'user')
RETURNING *;

-- name: CreateServiceIdentityAccount :one
INSERT INTO accounts (id, created_at, updated_at, account_type)
VALUES ( gen_random_uuid(), NOW(), NOW(), 'service_identity')
RETURNING *;
