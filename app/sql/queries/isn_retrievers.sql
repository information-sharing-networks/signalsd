-- name: CreateIsnRetriever :one
INSERT INTO isn_retrievers (
    isn_id,
    created_at,
    updated_at,
    default_rate_limit,
    retriever_status,
    listener_count
) VALUES ($1, now(), now(), $2, $3, $4)
RETURNING *;

-- name: UpdateIsnRetriever :execrows
UPDATE isn_retrievers SET (
  updated_at, 
  default_rate_limit,
  retriever_status,
  listener_count
) = (Now(), $2, $3, $4)
WHERE isn_id = $1;


-- name: GetForDisplayIsnRetrieverByIsnID :one
SELECT
    ir.created_at,
    ir.updated_at,
    ir.retriever_status,
    ir.default_rate_limit,
    ir.listener_count
FROM isn_retrievers ir
WHERE ir.isn_id = $1;


-- name: GetIsnRetrieverByIsnSlug :one
SELECT
    ir.*,
    i.is_in_use AS isn_is_in_use
FROM isn_retrievers ir
JOIN isn i 
ON i.id = ir.isn_id
WHERE i.slug = $1;

-- name: ExistsIsnRetriever :one

SELECT EXISTS
  (SELECT 1
   FROM isn_retrievers ir
   WHERE ir.isn_id = $1) AS EXISTS;
