-- name: CreateServiceAccount :one
INSERT INTO service_accounts (
    account_id,
    created_at,
    updated_at,
    client_id,
    client_contact_email,
    client_organization,
    rate_limit_per_minute,
    is_active
) VALUES ( $1, NOW(), NOW(), $2, $3, $4, $5, true)
RETURNING *;

-- name: CreateClientSecret :one
INSERT INTO client_secrets (hashed_secret, service_account_account_id, created_at, updated_at, expires_at)
VALUES ( $1,$2, NOW(), NOW(), $3 )
RETURNING hashed_secret, service_account_account_id;

-- name: CreateOneTimeClientSecret :one
INSERT INTO one_time_client_secrets (id, service_account_account_id, plaintext_secret, created_at, expires_at)
VALUES ( $1, $2, $3, NOW(), $4)
RETURNING id;

-- name: DeleteOneTimeClientSecret :execrows
DELETE from one_time_client_secrets 
WHERE id = $1;

-- name: GetValidClientSecretByServiceAccountAccountId :one
SELECT hashed_secret, expires_at
FROM client_secrets
WHERE service_account_account_id = $1
  AND revoked_at IS NULL
  AND expires_at > NOW();


-- name: RevokeClientSecret :execrows
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE hashed_secret = $1;

-- name: RevokeAllClientSecretsForUser :execrows
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE service_account_account_id = $1
AND revoked_at IS NULL;

-- name: ExistsServiceAccountWithEmailAndOrganization :one
SELECT EXISTS (
    SELECT 1 FROM service_accounts
    WHERE client_contact_email = $1
    AND client_organization = $2
) AS exists;

-- name: GetOneTimeClientSecret :one
SELECT created_at, service_account_account_id, plaintext_secret, expires_at
FROM one_time_client_secrets
WHERE id = $1;

-- name: GetValidClientSecretByHashedSecret :one
SELECT * FROM client_secrets
WHERE hashed_secret = $1
AND revoked_at IS NULL
AND expires_at > NOW();

-- name: GetServiceAccountByClientID :one
SELECT sa.* FROM service_accounts sa
WHERE sa.client_id = $1;

-- name: GetServiceAccountByAccountID :one
SELECT sa.* FROM service_accounts sa
WHERE sa.account_id = $1;
