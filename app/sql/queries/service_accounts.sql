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
                                    WHERE LOWER(client_organization) = LOWER(sqlc.arg(client_organization))
                                    AND LOWER(client_contact_email) = LOWER(sqlc.arg(client_contact_email)))
AND expires_at > NOW();

-- name: GetValidClientSecretByServiceAccountAccountId :one
-- only returns unrevoked/unexpired secrets (a secret scheduled for revocation remains valid until revoked_at)
SELECT hashed_secret, expires_at
FROM client_secrets
WHERE service_account_account_id = $1
  AND (revoked_at IS NULL OR revoked_at > NOW())
  AND expires_at > NOW();

-- name: RevokeClientSecret :execrows
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW(), NOW()) 
WHERE hashed_secret = $1;

-- name: RevokeAllClientSecretsForAccount :execrows
-- revokes immediately, including secrets that are scheduled for revocation (see ScheduleRevokeAllClientSecretsForAccount)
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW(), NOW())
WHERE service_account_account_id = $1
AND (revoked_at IS NULL OR revoked_at > NOW());

-- name: CountActiveClientSecrets :one
-- used in integration tests: counts the secrets that are not revoked (including secrets scheduled for revocation)
SELECT COUNT(*) as active_client_secrets 
FROM client_secrets
WHERE service_account_account_id = $1
AND (revoked_at IS NULL OR revoked_at > NOW());

-- name: ScheduleRevokeAllClientSecretsForAccount :execrows
-- used when a secret is rotated: the existing secrets remain valid for 5 minutes, so clients that have not yet received the new secret keep working.
-- (secrets that are already scheduled for revocation keep their existing revocation time)
UPDATE client_secrets SET (updated_at, revoked_at) = (NOW(), NOW() + INTERVAL '5 minutes')
WHERE service_account_account_id = $1
AND revoked_at IS NULL;


-- name: ExistsServiceAccountWithOrganizationAndEmail :one
SELECT exists
  (SELECT 1 FROM service_accounts
    WHERE LOWER(client_organization) = LOWER(sqlc.arg(client_organization))
    AND LOWER(client_contact_email) = LOWER(sqlc.arg(client_contact_email))) as exists;


-- name: GetServiceAccountWithOrganizationAndEmail :one
SELECT * FROM service_accounts
    WHERE LOWER(client_organization) = LOWER(sqlc.arg(client_organization))
    AND LOWER(client_contact_email) = LOWER(sqlc.arg(client_contact_email));

-- name: GetOneTimeClientSecret :one
SELECT created_at, service_account_account_id, plaintext_secret, expires_at
FROM one_time_client_secrets
WHERE id = $1;

-- name: GetValidClientSecretByHashedSecret :one
-- used for authentication: does not return expired or revoked credentials
-- (a secret scheduled for revocation remains valid until revoked_at)
--
-- the secret must belong to the service account being authenticated.
SELECT * FROM client_secrets
WHERE hashed_secret = $1
AND service_account_account_id = $2
AND (revoked_at IS NULL OR revoked_at > NOW())
AND expires_at > NOW();

-- name: GetNonRevokedClientSecretByHashedSecret :one
-- used for rotation: allows expired but not revoked credentials
-- (a secret scheduled for revocation can still be used to rotate until revoked_at, e.g. if the client did not receive the response to an earlier rotation)
SELECT * FROM client_secrets
WHERE hashed_secret = $1
AND service_account_account_id = $2
AND (revoked_at IS NULL OR revoked_at > NOW());

-- name: GetServiceAccountByClientID :one
SELECT sa.* FROM service_accounts sa
WHERE sa.client_id = $1;

-- name: GetServiceAccountByAccountID :one
SELECT sa.* FROM service_accounts sa
WHERE sa.account_id = $1;

-- name: GetServiceAccounts :many
SELECT sa.account_id, sa.created_at, sa.updated_at, sa.client_id, sa.client_contact_email, sa.client_organization 
FROM service_accounts sa;
