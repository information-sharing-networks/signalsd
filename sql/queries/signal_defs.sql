-- name: CreateSignalDef :one

INSERT INTO signal_defs (
    id,
    created_at,
    updated_at,
    user_id,
    isn_id,
    slug,
    schema_url,
    readme_url,
    title,
    detail,
    sem_ver,
    stage
) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6, $7, $8, $9) 
RETURNING *;


-- name: UpdateSignalDefDetails :execrows
UPDATE signal_defs SET (updated_at, readme_url, detail, stage) = (NOW(), $2, $3, $4)
WHERE id = $1;

-- name: DeleteSignalDef :execrows

DELETE
FROM signal_defs
WHERE id = $1;

-- name: GetSignalDefs :many

SELECT u.email,
       sd.*
FROM signal_defs sd
JOIN users u ON sd.user_id = u.id
ORDER BY u.email, 
         sd.slug,
         sd.sem_ver DESC;

-- name: GetSignalDefByID :one

SELECT u.email user_email,
       sd.*
FROM signal_defs sd
JOIN users u ON sd.user_id = u.id
WHERE sd.id = $1;


-- name: GetSignalDefBySlug :one

SELECT u.email user_email,
       sd.*
FROM signal_defs sd
JOIN users u ON sd.user_id = u.id
WHERE sd.slug = $1
AND sd.sem_ver = $2;

-- name: GetForDisplaySignalDefByID :one
SELECT 
    sd.id,
    sd.created_at,
    sd.updated_at,
    sd.slug,
    sd.schema_url,
    sd.readme_url,
    sd.title,
    sd.detail,
    sd.sem_ver,
    sd.stage
FROM signal_defs sd
WHERE sd.id = $1;

-- name: GetForDisplaySignalDefBySlug :one
SELECT 
    sd.id,
    sd.created_at,
    sd.updated_at,
    sd.slug,
    sd.schema_url,
    sd.readme_url,
    sd.title,
    sd.detail,
    sd.sem_ver,
    sd.stage
FROM signal_defs sd
WHERE sd.slug = $1
  AND sd.sem_ver = $2;

-- name: GetSemVerAndSchemaForLatestSlugVersion :one
-- if there are no signals defs for the supplied slug, this query returns an empty string for schema_url and a sem_ver of '0.0.0' 
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


-- name: ExistsSignalDefWithSlugAndDifferentUser :one

SELECT EXISTS
  (SELECT 1
   FROM signal_defs
   WHERE slug = $1
     AND user_id != $2) AS EXISTS;