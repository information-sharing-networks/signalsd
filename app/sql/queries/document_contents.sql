-- name: InsertDocumentContent :exec
-- documents are content-addressed within an account: storing the same content again is a no-op
INSERT INTO document_contents (
    account_id,
    sha256,
    created_at,
    content
) VALUES ($1, $2, now(), $3)
ON CONFLICT (account_id, sha256) DO NOTHING;

-- name: GetDocumentContent :one
SELECT content
FROM document_contents
WHERE account_id = $1
AND sha256 = $2;
