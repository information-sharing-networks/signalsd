-- name: CreateSignalDef :one

INSERT INTO signal_defs (id, created_at, updated_at, slug, schema_url, readme_url, title, detail, sem_ver, stage, user_id)
VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: GetSignalDefs :many

SELECT *
FROM signal_defs
ORDER BY created_at ASC;

-- name: GetSignalDef :one

SELECT *
FROM signal_defs
WHERE id = $1;

-- name: DeleteSignalDef :execrows

DELETE
FROM signal_defs
WHERE id = $1;

-- name: ExistsSignalDefWithSlugAndDifferentUser :one

SELECT EXISTS
  (SELECT 1
   FROM signal_defs
   WHERE slug = $1
     AND user_id != $2) AS EXISTS;

-- name: GetSemVerAndSchemaForLatestSlugVersion :one
-- this query will return an empty string for schema_url and a sem_ver of '0.0.0'if there are no signals defs for the supplied slug
SELECT '0.0.0' AS sem_ver,
       '' AS schema_url
WHERE NOT EXISTS
    (SELECT 1
     FROM signal_defs sd1
     WHERE sd1.slug = $1)
UNION ALL
SELECT sd2.sem_ver,
       sd2.schema_url
FROM signal_defs sd2
WHERE sd2.slug = $1
  AND sd2.sem_ver =
    (SELECT max(sd3.sem_ver)
     FROM signal_defs sd3
     WHERE sd3.slug = $1);