-- name: CreateSignal :one
-- this query creates one row in signals for every new combination of account_id, signal_type_id, local_ref
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
ON CONFLICT (account_id, signal_type_id, local_ref) DO NOTHING
RETURNING id;


-- name: CreateOrUpdateSignalWithCorrelationID :one
-- note if there is already a master record, then correlation_id is updated with the supplied value
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

-- Note the get queries:
-- do not return withdrawn or archived signals 
-- do not check validity status
-- require isn_slug,signal_type_slug & sem_ver params

-- name: GetLatestSignalVersionsByAccountID :many
WITH LatestSignals AS (
    SELECT
        a.id AS account_id,
        a.account_type,
        COALESCE(u.email, si.client_contact_email) AS email, -- show either the user or service account email
        s.local_ref,
        sv.version_number,
        sv.created_at,
        sv.id AS signal_version_id,
        sv.signal_id,
        s2.local_ref AS correlated_local_ref,
        s2.id AS correlated_signal_id,
        sv.content,
        ROW_NUMBER() OVER (PARTITION BY sv.signal_id ORDER BY sv.version_number DESC) AS rn
    FROM
        signal_versions sv
    JOIN
        signals s ON s.id = sv.signal_id
    JOIN
        signals s2 ON s2.id = s.correlation_id
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
    WHERE i.slug = sqlc.arg(isn_slug)
        AND st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
        AND i.is_in_use = true
        AND st.is_in_use = true
        AND s.account_id = sqlc.arg(account_id)
)
SELECT
    ls.account_id,
    ls.account_type,
    ls.email,
    ls.local_ref,
    ls.version_number,
    ls.created_at,
    ls.signal_version_id,
    ls.signal_id,
    ls.correlated_local_ref,
    ls.correlated_signal_id,
    ls.content
FROM
    LatestSignals ls
WHERE
    ls.rn = 1
ORDER BY
    ls.local_ref,
    ls.version_number,
    ls.signal_version_id;

-- name: GetLatestSignalVersionsByDateRange :many
WITH LatestSignals AS (
    SELECT
        a.id AS account_id,
        a.account_type,
        COALESCE(u.email, si.client_contact_email) AS email, -- show either the user or service account email
        s.local_ref,
        sv.version_number,
        sv.created_at,
        sv.id AS signal_version_id,
        sv.signal_id,
        s2.local_ref AS correlated_local_ref,
        s2.id AS correlated_signal_id,
        sv.content,
        ROW_NUMBER() OVER (PARTITION BY sv.signal_id ORDER BY sv.version_number DESC) AS rn
    FROM
        signal_versions sv
    JOIN
        signals s ON s.id = sv.signal_id
    JOIN
        signals s2 ON s2.id = s.correlation_id
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
    WHERE i.slug = sqlc.arg(isn_slug)
        AND st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
        AND i.is_in_use = true
        AND st.is_in_use = true
        AND sv.created_at BETWEEN sqlc.arg(start_date) AND sqlc.arg(end_date)
)
SELECT
    ls.account_id,
    ls.account_type,
    ls.email,
    ls.local_ref,
    ls.version_number,
    ls.created_at,
    ls.signal_version_id,
    ls.signal_id,
    ls.correlated_local_ref,
    ls.correlated_signal_id,
    ls.content
FROM
    LatestSignals ls
WHERE
    ls.rn = 1
ORDER BY
    ls.local_ref,
    ls.version_number,
    ls.signal_version_id;

-- name: GetLatestSignalVersionsByDateRangeAndAccountID :many
WITH LatestSignals AS (
    SELECT
        a.id AS account_id,
        a.account_type,
        COALESCE(u.email, si.client_contact_email) AS email, -- show either the user or service account email
        s.local_ref,
        sv.version_number,
        sv.created_at,
        sv.id AS signal_version_id,
        sv.signal_id,
        s2.local_ref AS correlated_local_ref,
        s2.id AS correlated_signal_id,
        sv.content,
        ROW_NUMBER() OVER (PARTITION BY sv.signal_id ORDER BY sv.version_number DESC) AS rn
    FROM
        signal_versions sv
    JOIN
        signals s ON s.id = sv.signal_id
    JOIN
        signals s2 ON s2.id = s.correlation_id
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
    WHERE i.slug = sqlc.arg(isn_slug)
        AND st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
        AND i.is_in_use = true
        AND st.is_in_use = true
        AND sv.created_at BETWEEN sqlc.arg(start_date) AND sqlc.arg(end_date)
        AND a.id = sqlc.arg(account_id)
)
SELECT
    ls.account_id,
    ls.account_type,
    ls.email,
    ls.local_ref,
    ls.version_number,
    ls.created_at,
    ls.signal_version_id,
    ls.signal_id,
    ls.correlated_local_ref,
    ls.correlated_signal_id,
    ls.content
FROM
    LatestSignals ls
WHERE
    ls.rn = 1
ORDER BY
    ls.local_ref,
    ls.version_number,
    ls.signal_version_id;


-- name: GetLatestSignalVersionsWithOptionalFilters :many
WITH LatestSignals AS (
    SELECT
        a.id AS account_id,
        a.account_type,
        COALESCE(u.email, si.client_contact_email) AS email, -- show either the user or service account email
        s.local_ref,
        sv.version_number,
        sv.created_at,
        sv.id AS signal_version_id,
        sv.signal_id,
        s2.local_ref AS correlated_local_ref,
        s2.id AS correlated_signal_id,
        sv.content,
        ROW_NUMBER() OVER (PARTITION BY sv.signal_id ORDER BY sv.version_number DESC) AS rn
    FROM
        signal_versions sv
    JOIN
        signals s ON s.id = sv.signal_id
    JOIN
        signals s2 ON s2.id = s.correlation_id
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
    WHERE i.slug = sqlc.arg(isn_slug)
        AND st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
        AND i.is_in_use = true
        AND st.is_in_use = true
        AND (sqlc.narg('account_id')::uuid IS NULL OR a.id = sqlc.narg('account_id')::uuid)
        AND (sqlc.narg('start_date')::timestamptz IS NULL OR sv.created_at >= sqlc.narg('start_date')::timestamptz)
        AND (sqlc.narg('end_date')::timestamptz IS NULL OR sv.created_at <= sqlc.narg('end_date')::timestamptz)
)
SELECT
    ls.account_id,
    ls.account_type,
    ls.email,
    ls.local_ref,
    ls.version_number,
    ls.created_at,
    ls.signal_version_id,
    ls.signal_id,
    ls.correlated_local_ref,
    ls.correlated_signal_id,
    ls.content
FROM
    LatestSignals ls
WHERE
    ls.rn = 1
ORDER BY
    ls.local_ref,
    ls.version_number,
    ls.signal_version_id;