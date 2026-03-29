-- name: CreateSignalType :one
INSERT INTO signal_types (
    id,
    created_at,
    updated_at,
    slug,
    schema_url,
    readme_url,
    title,
    detail,
    sem_ver,
    schema_content
    ) VALUES (gen_random_uuid(), now(), now(), $1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateSignalTypeDetails :execrows
UPDATE signal_types SET (updated_at, readme_url, detail) = (NOW(), $2, $3)
WHERE id = $1;


-- name: GetSignalTypes :many
SELECT st.*
FROM signal_types st;

-- name: GetSignalTypeBySlug :one
SELECT st.*
FROM signal_types st
WHERE st.slug = $1;

-- name: GetSignalTypesByIsnID :many
-- returns all signal types for the specified ISN 
-- check the is_in_use flag to see if the signal type is enabled for the ISN
SELECT st.*, ist.is_in_use
FROM signal_types st
JOIN isn_signal_types ist ON st.id = ist.signal_type_id
WHERE ist.isn_id = $1;

-- name: GetSignalTypeBySlugAndVersion :one
SELECT st.*
FROM signal_types st
WHERE st.slug = $1
AND st.sem_ver = $2;

-- name: GetSignalTypeByIsnIdAndSlug :one

SELECT st.*
FROM signal_types st
JOIN isn_signal_types ist ON st.id = ist.signal_type_id
WHERE ist.isn_id = $1
AND st.slug = $2
AND st.sem_ver = $3;

-- if there are no signals defs for the supplied slug, this query returns an empty string for schema_url and a sem_ver of '0.0.0'
-- name: GetLatestSlugVersion :one
SELECT '0.0.0' AS sem_ver,
       '' AS schema_url,
       '' AS title
WHERE NOT EXISTS
    (SELECT 1
     FROM signal_types st1
     WHERE st1.slug = $1)
UNION ALL
SELECT st2.sem_ver,
       st2.schema_url,
       st2.title
FROM signal_types st2
WHERE st2.slug = $1
  AND st2.sem_ver =
    (SELECT max(st3.sem_ver)
     FROM signal_types st3
     WHERE st3.slug = $1);

-- name: GetInUseSignalTypesByIsnID :many
-- only returns active signal_types (is_in_use = true).
SELECT st.*
FROM isn i
JOIN isn_signal_types ist ON ist.isn_id = i.id
JOIN signal_types st ON st.id = ist.signal_type_id
WHERE i.id = $1
AND i.is_in_use = true
AND ist.is_in_use = true;


-- name: GetInUsePublicIsnSignalTypes :many
-- only returns active ISNs and signal types (is_in_use = true), and checks ISN-level signal type status
SELECT
    i.slug as isn_slug,
    st.slug as signal_type_slug,
    st.sem_ver
FROM isn i
JOIN isn_signal_types ist ON ist.isn_id = i.id
JOIN signal_types st ON st.id = ist.signal_type_id
WHERE i.visibility = 'public'
AND i.is_in_use = true
AND ist.is_in_use = true;

-- name: ExistsSignalTypeWithSlugAndSchema :one
SELECT EXISTS
  (SELECT 1
   FROM signal_types
   WHERE slug = $1
   AND schema_url = $2) AS EXISTS;


-- name: CheckSignalTypeHasSignals :one
SELECT EXISTS(
    SELECT 1
    FROM signals
    WHERE signal_type_id = $1
) AS in_use;

-- name: DeleteSignalType :execrows
DELETE FROM signal_types
WHERE id = $1;

-- name: AddSignalTypeToIsn :exec
INSERT INTO isn_signal_types (isn_id, signal_type_id, created_at, updated_at)
VALUES ($1, $2, now(), now())
ON CONFLICT (isn_id, signal_type_id) DO NOTHING;

-- name: UpdateIsnSignalTypeStatus :execrows
UPDATE isn_signal_types SET (updated_at, is_in_use) = (NOW(), $3)
WHERE isn_id = $1 AND signal_type_id = $2;

-- name: IsSignalTypeEnabledForIsn :one
SELECT ist.is_in_use
FROM isn_signal_types ist
WHERE ist.isn_id = $1 AND ist.signal_type_id = $2;