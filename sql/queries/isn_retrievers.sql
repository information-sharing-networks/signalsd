-- name: CreateIsnRetriever :one
INSERT INTO isn_retrievers (
    id,
    created_at,
    updated_at,
    user_id,
    isn_id,
    title,
    detail,
    slug,
    retriever_origin,
    retriever_status,
    default_rate_limit
) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, slug;

-- name: UpdateIsnRetriever :execrows
UPDATE isn_retrievers SET (
  updated_at, 
  detail,
  retriever_origin,
  default_rate_limit,
  retriever_status
) = (Now(), $2, $3, $4, $5)
WHERE id = $1;

-- name: GetIsnRetrieverBySlug :one
SELECT
    i.slug AS isn_slug,
    i.is_in_use AS isn_is_in_use,
    i.storage_type AS isn_storage_type,
    ir.*
FROM isn_retrievers ir
JOIN isn i ON i.id = ir.isn_id
WHERE ir.slug = $1;

-- name: GetForDisplayIsnRetrieversByIsnID :many
SELECT
    ir.id,
    ir.created_at,
    ir.updated_at,
    ir.title,
    ir.detail,
    ir.slug,
    ir.retriever_origin,
    ir.retriever_status,
    ir.default_rate_limit
FROM isn_retrievers ir
WHERE ir.isn_id = $1;

-- name: ExistsIsnRetrieverWithSlug :one

SELECT EXISTS
  (SELECT 1
   FROM isn_retrievers
   WHERE slug = $1) AS EXISTS;
