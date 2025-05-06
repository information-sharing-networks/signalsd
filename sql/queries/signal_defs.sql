-- name: CreateSignalDef :one
INSERT INTO signal_defs (id, created_at, updated_at, slug, schema_url, readme_url, title, detail, sem_ver, stage, user_id)
VALUES ( gen_random_uuid(), NOW(), NOW(), $1, $2, $3, $4, $5, $6, $7, $8 )
RETURNING *;

-- name: GetSignalDefs :many
SELECT * FROM signal_defs ORDER BY created_at ASC;

-- name: GetSignalDef :one
SELECT * FROM signal_defs WHERE id = $1;

-- name: DeleteSignalDef :execrows
DELETE FROM signal_defs 
WHERE id = $1;