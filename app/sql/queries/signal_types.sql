-- name: CreateSignalType :one

INSERT INTO signal_types (
    id,
    created_at,
    updated_at,
    isn_id,
    slug,
    schema_url,
    readme_url,
    title,
    detail,
    sem_ver,
    is_in_use,
    schema_content
    ) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6, $7, true, $8)
RETURNING *;

-- name: UpdateSignalTypeDetails :execrows
UPDATE signal_types SET (updated_at, readme_url, detail, is_in_use) = (NOW(), $2, $3, $4)
WHERE id = $1;

-- name: GetSignalTypes :many

SELECT st.*
FROM signal_types st;

-- name: GetSignalTypeByID :one

SELECT st.*
FROM signal_types st
WHERE st.id = $1;

-- name: GetSignalTypeBySlug :one

SELECT st.*
FROM signal_types st
WHERE st.slug = $1
AND st.sem_ver = $2;

-- name: GetForDisplaySignalTypeByID :one
SELECT 
    st.id,
    st.created_at,
    st.updated_at,
    st.slug,
    st.schema_url,
    st.readme_url,
    st.title,
    st.detail,
    st.sem_ver,
    st.is_in_use
FROM signal_types st
WHERE st.id = $1;

-- name: GetForDisplaySignalTypeBySlug :one
SELECT 
    st.id,
    st.created_at,
    st.updated_at,
    st.slug,
    st.schema_url,
    st.readme_url,
    st.title,
    st.detail,
    st.sem_ver,
    st.is_in_use
FROM signal_types st
WHERE st.slug = $1
  AND st.sem_ver = $2;


-- name: GetForDisplaySignalTypeByIsnID :many
SELECT 
    st.id,
    st.created_at,
    st.updated_at,
    st.slug,
    st.schema_url,
    st.readme_url,
    st.title,
    st.detail,
    st.sem_ver,
    st.is_in_use
FROM signal_types st
WHERE st.isn_id = $1;

-- if there are no signals defs for the supplied slug, this query returns an empty string for schema_url and a sem_ver of '0.0.0' 
-- name: GetSemVerAndSchemaForLatestSlugVersion :one
SELECT '0.0.0' AS sem_ver,
       '' AS schema_url
WHERE NOT EXISTS
    (SELECT 1
     FROM signal_types st1
     WHERE st1.slug = $1)
UNION ALL
SELECT st2.sem_ver,
       st2.schema_url
FROM signal_types st2
WHERE st2.slug = $1
  AND st2.sem_ver =
    (SELECT max(st3.sem_ver)
     FROM signal_types st3
     WHERE st3.slug = $1);

-- only return signal_types for the ISN that are flagged "in use"
-- name: GetInUseSignalTypesByIsnID :many
SELECT st.*
FROM signal_types st
WHERE st.isn_id = $1
AND is_in_use = true;

-- name: ExistsSignalTypeWithSlugAndSchema :one
SELECT EXISTS
  (SELECT 1
   FROM signal_types
   WHERE slug = $1
   AND schema_url = $2) AS EXISTS;

-- name: GetSchemaURLs :many
SELECT DISTINCT schema_url FROM signal_types;

-- name: CheckSignalTypeInUse :one
SELECT EXISTS(
    SELECT 1
    FROM signals
    WHERE signal_type_id = $1
) AS in_use;

-- name: DeleteSignalType :execrows
DELETE FROM signal_types
WHERE id = $1;