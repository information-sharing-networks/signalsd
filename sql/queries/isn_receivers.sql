-- name: CreateIsnReceiver :one
INSERT INTO isn_receivers (
    id,
    created_at,
    updated_at,
    user_id,
    isn_id,
    title,
    detail,
    slug,
    receiver_origin,
    min_batch_records,
    max_batch_records,
    max_daily_validation_failures,
    max_payload_kilobytes,
    payload_validation,
    default_rate_limit,
    receiver_status
) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) 
RETURNING id, slug;

-- name: UpdateIsnReceiver :execrows
UPDATE isn_receivers SET (
  updated_at, 
  detail,
  receiver_origin,
  min_batch_records,
  max_batch_records,
  max_daily_validation_failures,
  max_payload_kilobytes,
  payload_validation,
  default_rate_limit,
  receiver_status
) = (Now(), $2, $3, $4, $5, $6, $7, $8, $9, $10)
WHERE id = $1;

-- name: GetIsnReceiverBySlug :one
SELECT
    i.slug AS isn_slug,
    i.is_in_use AS isn_is_in_use,
    i.storage_type AS isn_storage_type,
    ir.*
FROM isn_receivers ir
JOIN isn i ON i.id = ir.isn_id
WHERE ir.slug = $1;

-- name: ExistsIsnReceiverWithSlug :one

SELECT EXISTS
  (SELECT 1
   FROM isn_receivers
   WHERE slug = $1) AS EXISTS;
