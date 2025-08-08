-- name: CreateServiceAccount :one
INSERT INTO service_accounts (
    account_id,
    created_at,
    updated_at,
    client_id,
    client_contact_email,
    client_organization
) VALUES ( $1, NOW(), NOW(), $2, $3, $4)
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

-- name: DeleteOneTimeClientSecretsByOrgAndEmail :execrows
DELETE from one_time_client_secrets 
WHERE service_account_account_id = (SELECT account_id 
                                    FROM service_accounts 
                                    WHERE client_organization = $1 
                                    AND client_contact_email = $2)
AND expires_at > NOW();

-- name: GetValidClientSecretByServiceAccountAccountId :one
-- only returns unrevoked/unexpired secrets
SELECT hashed_secret, expires_at
FROM client_secrets
WHERE service_account_account_id = $1
  AND revoked_at IS NULL
  AND expires_at > NOW();

-- name: RevokeClientSecret :execrows
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE hashed_secret = $1;

-- name: RevokeAllClientSecretsForAccount :execrows
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW(), NOW())
WHERE service_account_account_id = $1
AND revoked_at IS NULL;

-- name: CountActiveClientSecrets :one
-- used in integration tests
SELECT COUNT(*) as active_client_secrets 
FROM client_secrets
WHERE service_account_account_id = $1
AND revoked_at IS NOT NULL;

-- name: ScheduleRevokeAllClientSecretsForAccount :execrows
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW() + INTERVAL '5 minutes', NOW())
WHERE service_account_account_id = $1
AND revoked_at IS NULL;

-- name: ExistsServiceAccountWithEmailAndOrganization :one
SELECT EXISTS (
    SELECT 1 FROM service_accounts
    WHERE client_contact_email = $1
    AND client_organization = $2
) AS exists;

-- name: GetServiceAccountWithOrganizationAndEmail :one
SELECT * FROM service_accounts
    WHERE client_organization = $1
    AND client_contact_email = $2;

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

-- name: GetServiceAccounts :many
SELECT sa.account_id, sa.created_at, sa.updated_at, sa.client_id, sa.client_contact_email, sa.client_organization 
FROM service_accounts sa;
