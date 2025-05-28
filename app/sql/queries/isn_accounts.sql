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

-- get all the isns an account has access to.
-- name: GetIsnAccountsByAccountID :many
SELECT ia.*, i.slug as isn_slug FROM isn_accounts ia
JOIN isn i 
ON i.id = ia.isn_id
WHERE ia.account_id = $1;