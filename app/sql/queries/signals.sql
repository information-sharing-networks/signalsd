-- name: CreateSignal :one
-- this query creates one row in the signals table for every new combination of account_id, signal_type_id, local_ref.
-- if a withdrawn signal is received again it is reactivated (is_withdrawn = false).
-- returns the signal_id.
WITH ids AS (
    SELECT st.id AS signal_type_id, 
        st.isn_id,
        gen_random_uuid() AS signal_id
    FROM signal_types st 
    WHERE st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
)
INSERT INTO signals (
    id,
    created_at,
    updated_at,
    account_id,
    isn_id,
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
    ids.isn_id,
    ids.signal_type_id,
    sqlc.arg(local_ref),
    ids.signal_id,
    false,
    false
FROM ids
-- deactivated records (is_withdrawn = true) are reactivated by resubmitting them - the update below ensures the updated_at timestamp is only changed if the record is reactivated
-- the only other signals field that can be updated is the correlation_id (handled by CreateOrUpdateSignalWithCorrelationID)
ON CONFLICT (account_id, signal_type_id, local_ref)
DO UPDATE SET
    is_withdrawn = false,
    updated_at = CASE 
        WHEN signals.is_withdrawn = true THEN now()
        ELSE signals.updated_at
    END
RETURNING id;

-- name: CreateOrUpdateSignalWithCorrelationID :one
-- note if there is already a master record for this local_ref, then:
-- 1. correlation_id is updated with the supplied value (assuming it is different to the existing value)
-- 2. if the signal was withdrawn it is reactivated (is_withdrawn = false)
WITH ids AS (
    SELECT 
        st.id AS signal_type_id,
        st.isn_id
    FROM signal_types st 
    WHERE st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
)
INSERT INTO signals (
    id,
    created_at,
    updated_at,
    account_id,
    isn_id,
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
    ids.isn_id,
    ids.signal_type_id,
    sqlc.arg(local_ref),
    sqlc.arg(correlation_id),
    false,
    false
FROM ids 
ON CONFLICT (account_id, signal_type_id, local_ref)
DO UPDATE SET
    correlation_id = CASE
        WHEN signals.correlation_id != EXCLUDED.correlation_id THEN EXCLUDED.correlation_id
        ELSE signals.correlation_id
    END,
    is_withdrawn = CASE
        WHEN signals.is_withdrawn = true THEN false
        ELSE signals.is_withdrawn
    END,
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
    content
)
SELECT
    gen_random_uuid(),
    now(), 
    sqlc.arg(account_id),
    sqlc.arg(signal_batch_id),
    s.id,
    ver.version_number,
    sqlc.arg(content)
FROM ver 
JOIN signals s 
    ON s.signal_type_id = ver.signal_type_id
    AND s.account_id = sqlc.arg(account_id)
    AND s.local_ref = sqlc.arg(local_ref)
RETURNING id, version_number;

-- name: WithdrawSignalByID :execrows
UPDATE signals
SET is_withdrawn = true, updated_at = NOW()
WHERE id = $1;

-- name: WithdrawSignalByLocalRef :execrows
UPDATE signals
SET is_withdrawn = true, updated_at = NOW()
WHERE account_id = $1
    AND signal_type_id = (
        SELECT st.id
        FROM signal_types st
        WHERE st.slug = $2 AND st.sem_ver = $3
    )
    AND local_ref = $4;

-- name: GetSignalsWithOptionalFilters :many
-- you must supply the isn_slug,signal_type_slug & sem_ver params - other filters are optional
-- signals for inactive isns or signal_types are not returned (is_in_use = false)
SELECT
 a.id AS account_id,
    a.account_type,
    COALESCE(u.email, si.client_contact_email) AS email,
    s.id as signal_id,
    s.local_ref,
    s.created_at signal_created_at,
    lsv.id AS signal_version_id,
    lsv.version_number,
    lsv.created_at version_created_at,
    s.correlation_id as correlated_to_signal_id,
    s.is_withdrawn,
    lsv.content
FROM
    latest_signal_versions lsv
JOIN
    signals s ON s.id = lsv.signal_id
JOIN
    accounts a ON a.id = s.account_id
JOIN
    signal_types st on st.id = s.signal_type_id
JOIN
    isn i ON i.id = st.isn_id
LEFT OUTER JOIN
    users u ON u.account_id = a.id
LEFT OUTER JOIN
    service_accounts si ON si.account_id = a.id
WHERE
    i.slug = sqlc.arg(isn_slug)
    AND st.slug = sqlc.arg(signal_type_slug)
    AND st.sem_ver = sqlc.arg(sem_ver)
    AND i.is_in_use = true
    AND st.is_in_use = true
    AND (sqlc.narg('include_withdrawn')::boolean = true OR s.is_withdrawn = false)
    AND (sqlc.narg('account_id')::uuid IS NULL OR a.id = sqlc.narg('account_id')::uuid)
    AND (sqlc.narg('signal_id')::uuid IS NULL OR s.id = sqlc.narg('signal_id')::uuid)
    AND (sqlc.narg('local_ref')::text IS NULL OR s.local_ref = sqlc.narg('local_ref')::text)
    AND (sqlc.narg('start_date')::timestamptz IS NULL OR lsv.created_at >= sqlc.narg('start_date')::timestamptz)
    AND (sqlc.narg('end_date')::timestamptz IS NULL OR lsv.created_at <= sqlc.narg('end_date')::timestamptz)
ORDER BY
    lsv.created_at,
    s.local_ref,
    lsv.version_number;

-- name: GetSignalByAccountAndLocalRef :one
SELECT s.*, i.slug as isn_slug, st.slug as signal_type_slug, st.sem_ver
FROM signals s
JOIN signal_types st ON st.id = s.signal_type_id
JOIN isn i ON i.id = s.isn_id
WHERE s.account_id = $1
    AND st.slug = $2
    AND st.sem_ver = $3
    AND s.local_ref = $4;

-- name: ValidateCorrelationID :one
SELECT EXISTS(
    SELECT 1
    FROM signals s
    JOIN signal_types st ON st.id = s.signal_type_id
    JOIN isn i ON i.id = st.isn_id
    WHERE s.id = sqlc.arg(correlation_id)
        AND i.slug = sqlc.arg(isn_slug)
) AS is_valid;

-- name: GetSignalCorrelationDetails :one
-- Get signal with its correlation details for verification during integration tests
SELECT
    s.id,
    s.local_ref,
    s.correlation_id,
    s.isn_id,
    i.slug as isn_slug,
    sc.local_ref as correlated_local_ref,
    sc.id as correlated_signal_id
FROM signals s
JOIN signal_types st ON st.id = s.signal_type_id
JOIN isn i ON i.id = s.isn_id
join signals sc on sc.id = s.correlation_id
WHERE s.account_id = $1
    AND st.slug = $2
    AND st.sem_ver = $3
    AND s.local_ref = $4;

-- name: GetSignalsByCorrelationIDs :many
-- Get all signals that correlate to the provided signal IDs (for embedding correlated signals)
-- Signals for inactive isns or signal types (is_in_use = false) are not returned
SELECT
    a.id AS account_id,
    a.account_type,
    COALESCE(u.email, si.client_contact_email) AS email,
    s.id as signal_id,
    s.local_ref,
    s.created_at signal_created_at,
    lsv.id AS signal_version_id,
    lsv.version_number,
    lsv.created_at version_created_at,
    s.correlation_id as correlated_to_signal_id,
    s.is_withdrawn,
    lsv.content
FROM
    latest_signal_versions lsv
JOIN
    signals s ON s.id = lsv.signal_id
JOIN
    accounts a ON a.id = s.account_id
JOIN
    signal_types st on st.id = s.signal_type_id
JOIN
    isn i ON i.id = st.isn_id
LEFT OUTER JOIN
    users u ON u.account_id = a.id
LEFT OUTER JOIN
    service_accounts si ON si.account_id = a.id
WHERE
    s.correlation_id = ANY(sqlc.slice(correlation_ids))
    AND s.correlation_id != s.id  -- exclude self-referencing signals
    AND i.is_in_use = true
    AND st.is_in_use = true
    AND (sqlc.narg('include_withdrawn')::boolean = true OR s.is_withdrawn = false)
ORDER BY
    s.correlation_id,
    s.local_ref,
    lsv.version_number,
    lsv.id;

-- name: GetPreviousSignalVersions :many
-- get the previous versions for the supplied signals (no rows returned if the signal only has 1 version)
SELECT sv.signal_id, id as signal_version_id, sv.created_at, sv.version_number, sv.content
FROM signal_versions  sv
WHERE
    sv.signal_id = ANY(sqlc.slice(signal_ids))
    -- exclude the latest version 
    AND sv.id != (SELECT id from latest_signal_versions lsv WHERE lsv.signal_id = sv.signal_id)
    ORDER BY sv.created_at;
