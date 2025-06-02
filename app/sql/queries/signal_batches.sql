-- name: CreateSignalBatch :one
INSERT INTO signal_batches (
    id,
    created_at,
    updated_at,
    isn_id,
    account_id,
    is_latest,
    account_type
) VALUES (
    gen_random_uuid(), 
    now(), 
    now(), 
    $1, 
    $2, 
    TRUE,
    $3
)
RETURNING *;

-- create a batch for the owner on a new ISN created by an admin
-- name: CreateOwnerSignalBatch :one
INSERT INTO signal_batches (
    id,
    created_at,
    updated_at,
    isn_id,
    account_id,
    is_latest,
    account_type
) VALUES (
    gen_random_uuid(), 
    now(), 
    now(), 
    $1, 
    (select account_id from users where user_role = 'owner'),
    TRUE,
    $2
)
RETURNING *;

-- name: CloseISNSignalBatchByAccountID :execrows
UPDATE signal_batches 
SET is_latest = FALSE
WHERE isn_id = $1 and account_id = $2;

-- name: GetLatestIsnSignalBatchesByAccountID :many
SELECT sb.*, i.slug as isn_slug FROM signal_batches sb 
JOIN isn i
    ON sb.isn_id = i.id
WHERE account_id = $1
AND is_latest = TRUE;

-- name: GetLatestSignalBatchByIsnSlugAndBatchID :one
SELECT sb.*, i.slug as isn_slug FROM signal_batches sb
JOIN isn i 
ON i.id = sb.isn_id
WHERE i.slug = $1 
AND sb.id = $2
AND sb.is_latest = TRUE;

