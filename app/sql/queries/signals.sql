-- name: CreateSignal :one
-- this query creates one row in signals for every new combination of account_id, signal_type_id, local_ref
WITH ids AS (
    SELECT st.id AS signal_type_id, gen_random_uuid() AS signal_id
    FROM signal_types st 
    WHERE st.slug = sqlc.arg(signal_type_slug)
    and st.sem_ver = sqlc.arg(sem_ver)
)
INSERT INTO signals (
    id,
    created_at,
    updated_at,
    account_id,
    signal_type_id,
    local_ref,
    correlation_id,
    is_withdrawn,
    is_archived)
SELECT
    ids.signal_id,
    now(),
    now(),
    sqlc.arg(account_id),
    ids.signal_type_id,
    sqlc.arg(local_ref),
    ids.signal_id,
    false,
    false
FROM ids
ON CONFLICT (account_id, signal_type_id, local_ref) DO NOTHING
RETURNING id;


-- name: CreateOrUpdateSignalWithCorrelationID :one
-- note if there is already a master record, then correlation_id is updated with the supplied value
WITH ids AS (
    SELECT st.id AS signal_type_id
    FROM signal_types st 
    WHERE st.slug = sqlc.arg(signal_type_slug)
    and st.sem_ver = sqlc.arg(sem_ver)
)
INSERT INTO signals (
    id,
    created_at,
    updated_at,
    account_id,
    signal_type_id,
    local_ref,
    correlation_id,
    is_withdrawn,
    is_archived)
SELECT
    gen_random_uuid(),
    now(),
    now(),
    sqlc.arg(account_id),
    ids.signal_type_id,
    sqlc.arg(local_ref),
    sqlc.arg(correlation_id),
    false,
    false
FROM ids 
ON CONFLICT (account_id, signal_type_id, local_ref) 
DO UPDATE SET 
    correlation_id = EXCLUDED.correlation_id,
    updated_at = now()
RETURNING id;

-- name: CreateSignalVersion :one
-- if there is already a version of this signal, create a new one with an incremented version_number
WITH ver AS (
    SELECT 
        st.id AS signal_type_id,
        COALESCE(
            (SELECT MAX(sv.version_number) 
             FROM signal_versions sv 
             JOIN signals s
                ON s.id = sv.signal_id
             WHERE s.local_ref = sqlc.arg(local_ref))
            , 0) + 1 as version_number
    FROM signal_types st
    WHERE st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
)
INSERT INTO signal_versions (
    id,
    created_at,
    account_id,
    signal_batch_id,
    signal_id,
    version_number,
    validation_status,
    content
)
SELECT
    gen_random_uuid(),
    now(), 
    sqlc.arg(account_id),
    sqlc.arg(signal_batch_id),
    s.id,
    ver.version_number,
    'pending',
    sqlc.arg(content)
FROM ver 
JOIN signals s 
    ON s.signal_type_id = ver.signal_type_id
    AND s.account_id = sqlc.arg(account_id)
    AND s.local_ref = sqlc.arg(local_ref)
RETURNING id, version_number;