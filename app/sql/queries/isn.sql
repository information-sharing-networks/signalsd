-- name: CreateIsn :one

INSERT INTO isn (
    id,
    created_at,
    updated_at,
    user_account_id,
    title,
    slug,
    detail,
    is_in_use,
    visibility
) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6 )
RETURNING id, slug;

-- name: UpdateIsn :execrows
UPDATE isn SET (
    updated_at, 
    detail,
    is_in_use,
    visibility
) = (Now(), $2, $3, $4)
WHERE id = $1;

-- name: GetIsnByID :one
SELECT i.* 
FROM isn i
WHERE i.id = $1;

-- name: GetIsnBySlug :one
SELECT i.* 
FROM isn i
WHERE i.slug = $1;

-- name: GetIsnBySignalTypeID :one
SELECT i.* 
FROM isn i
JOIN signal_types sd on sd.isn_id = i.id
WHERE sd.id = $1;

-- name: GetIsns :many
SELECT i.* 
FROM isn i;

-- name: GetForDisplayIsnBySlug :one
SELECT 
    id,
    created_at,
    updated_at,
    title,
    slug,
    detail,
    is_in_use,
    visibility
FROM isn 
WHERE slug = $1;

-- name: ExistsIsnWithSlug :one

SELECT EXISTS
  (SELECT 1
   FROM isn
   WHERE slug = $1) AS EXISTS;