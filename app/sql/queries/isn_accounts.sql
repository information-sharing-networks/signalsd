-- name: CreateIsnAccount :one
INSERT INTO isn_accounts (
    id,
    created_at,
    updated_at,
    isn_id,
    account_id,
    permission
) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3)
RETURNING *;

-- name: UpdateIsnAccount :one
UPDATE isn_accounts SET 
    updated_at = now(),
    permission = $3
WHERE isn_id =  $1
AND account_id = $2
RETURNING *;


-- name: DeleteIsnAccount :execrows
DELETE FROM isn_accounts
WHERE isn_id =  $1
AND account_id = $2;

-- name: GetIsnAccountByIsnAndAccountID :one
SELECT ia.*, i.slug as isn_slug FROM isn_accounts ia
JOIN isn i 
ON i.id = ia.isn_id
WHERE ia.isn_id = $1 
AND ia.account_id = $2;

-- name: GetActiveIsnAccountsByAccountID :many
-- get all the active isns an account has access to.
SELECT ia.*, i.slug as isn_slug FROM isn_accounts ia
JOIN isn i
ON i.id = ia.isn_id
WHERE ia.account_id = $1
and i.is_in_use = true;

-- get all accounts that have access to a specific ISN
-- name: GetAccountsByIsnID :many
SELECT
    ia.id,
    ia.created_at,
    ia.updated_at,
    ia.isn_id,
    ia.account_id,
    ia.permission,
    a.account_type,
    a.is_active,
    COALESCE(u.email, sa.client_contact_email) AS email,
    COALESCE(u.user_role, 'member') AS account_role,
    sa.client_id,
    sa.client_organization
FROM isn_accounts ia
JOIN accounts a ON a.id = ia.account_id
LEFT OUTER JOIN users u ON u.account_id = ia.account_id
LEFT OUTER JOIN service_accounts sa ON sa.account_id = ia.account_id
WHERE ia.isn_id = $1
ORDER BY a.account_type, COALESCE(u.email, sa.client_contact_email);