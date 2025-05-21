-- name: CreateIsnReceiver :one
INSERT INTO isn_receivers (
    isn_id,
    created_at,
    updated_at,
    max_daily_validation_failures,
    max_payload_kilobytes,
    payload_validation,
    default_rate_limit,
    receiver_status, 
    listener_count
) VALUES ($1, now(), now(), $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateIsnReceiver :execrows
UPDATE isn_receivers SET (
  updated_at, 
  max_daily_validation_failures,
  max_payload_kilobytes,
  payload_validation,
  default_rate_limit,
  receiver_status,
  listener_count
) = (Now(), $2, $3, $4, $5, $6, $7)
WHERE isn_id = $1;

-- name: GetForDisplayIsnReceiverByIsnID :one
SELECT
    ir.created_at,
    ir.updated_at,
    ir.max_daily_validation_failures,
    ir.max_payload_kilobytes,
    ir.payload_validation,
    ir.default_rate_limit,
    ir.receiver_status,
    ir.listener_count
FROM isn_receivers ir
WHERE ir.isn_id = $1;

-- name: GetIsnReceiverByIsnSlug :one

SELECT ir.* , i.is_in_use as isn_is_in_use
FROM isn_receivers ir
JOIN isn i
ON i.id = ir.isn_id
WHERE i.slug = $1;

-- name: ExistsIsnReceiver :one

SELECT EXISTS
  (SELECT 1
   FROM isn_receivers ir
   WHERE ir.isn_id = $1) AS EXISTS;