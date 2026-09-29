-- name: CreateSignal :one
-- This query creates one row in the signals table for every new combination of account_id, signal_type_id, local_ref.
-- If a withdrawn signal is received again it is reactivated (is_withdrawn = false).
-- Only creates signals if ISN and signal type are in use (this is a defence against stale access tokens).
-- Returns the new signal_id.
WITH ids AS (
    SELECT st.id AS signal_type_id,
        i.id AS isn_id,
       uuidv7() AS signal_id
    FROM signal_types st
    JOIN isn_signal_types ist ON st.id = ist.signal_type_id
    JOIN isn i ON i.id = ist.isn_id
    WHERE i.slug = sqlc.arg(isn_slug)
        AND st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
        AND i.is_in_use = true
        AND ist.is_in_use = true
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
-- deactivated records (is_withdrawn = true) are reactivated by resubmitting them.
-- updated_at is changed on every resubmission, since a new version is created (search uses it to find the signals that changed since a given time - updated_since)
-- the only other signals field that can be updated is the correlation_id (handled by CreateOrUpdateSignalWithCorrelationID)
ON CONFLICT (account_id, signal_type_id, local_ref)
DO UPDATE SET
    is_withdrawn = false,
    updated_at = now()
RETURNING id;

-- name: CreateOrUpdateSignalWithCorrelationID :one
-- note if there is already a master record for this local_ref, then:
-- 1. correlation_id is updated with the supplied value (assuming it is different to the existing value)
-- 2. if the signal was withdrawn it is reactivated (is_withdrawn = false).
-- Only creates/updates signals if ISN and signal type are in use (this is a defence against stale access tokens).
WITH ids AS (
    SELECT st.id AS signal_type_id,
        i.id AS isn_id,
       uuidv7() AS signal_id
    FROM signal_types st
    JOIN isn_signal_types ist ON st.id = ist.signal_type_id
    JOIN isn i ON i.id = ist.isn_id
    WHERE i.slug = sqlc.arg(isn_slug)
        AND st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
        AND i.is_in_use = true
        AND ist.is_in_use = true
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
   uuidv7(),
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

-- name: CreateEventSignal :one
-- Creates the signal master record for an event. Events are immutable, so unlike CreateSignal and CreateOrUpdateSignalWithCorrelationID
-- an existing signal is never updated (not reactivated or recorrelated).
-- Returns no rows if the account already has an event with this local_ref (use CompareWithLatestSignalVersion to compare it with the resubmitted event),
-- or if the ISN or signal type is not in use (this is a defence against stale access tokens).
WITH ids AS (
    SELECT st.id AS signal_type_id,
        i.id AS isn_id
    FROM signal_types st
    JOIN isn_signal_types ist ON st.id = ist.signal_type_id
    JOIN isn i ON i.id = ist.isn_id
    WHERE i.slug = sqlc.arg(isn_slug)
        AND st.slug = sqlc.arg(signal_type_slug)
        AND st.sem_ver = sqlc.arg(sem_ver)
        AND i.is_in_use = true
        AND ist.is_in_use = true
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
    uuidv7(),
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
ON CONFLICT (account_id, signal_type_id, local_ref) DO NOTHING
RETURNING id;

-- name: CompareWithLatestSignalVersion :one
-- returns the latest version of the account's signal with the supplied local_ref, and whether its content is the same as the supplied content
-- (compared as jsonb, so key order and whitespace are ignored). Used to detect unchanged json and event resubmissions.
SELECT
    s.id AS signal_id,
    s.correlation_id,
    s.is_withdrawn,
    lsv.id AS signal_version_id,
    lsv.version_number,
    (lsv.content = sqlc.arg(content)::jsonb)::boolean AS content_matches
FROM signals s
JOIN signal_types st ON st.id = s.signal_type_id
JOIN latest_signal_versions lsv ON lsv.signal_id = s.id
WHERE s.account_id = sqlc.arg(account_id)
    AND st.slug = sqlc.arg(signal_type_slug)
    AND st.sem_ver = sqlc.arg(sem_ver)
    AND s.local_ref = sqlc.arg(local_ref);

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
             WHERE s.local_ref = sqlc.arg(local_ref)
                AND s.account_id = sqlc.arg(account_id)
                AND s.signal_type_id = st.id)
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
   uuidv7(),
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
-- updated_at is only changed if the signal was not already withdrawn (so a repeated withdrawal is not reported as a change by updated_since)
UPDATE signals
SET is_withdrawn = true,
    updated_at = CASE WHEN is_withdrawn THEN updated_at ELSE NOW() END
WHERE id = sqlc.arg(id);

-- name: WithdrawSignalByLocalRef :execrows
-- Only withdraws signals if ISN and signal type are in use (this is a defence against stale access tokens).
-- updated_at is only changed if the signal was not already withdrawn (so a repeated withdrawal is not reported as a change by updated_since)
UPDATE signals
SET is_withdrawn = true,
    updated_at = CASE WHEN is_withdrawn THEN updated_at ELSE NOW() END
WHERE account_id = sqlc.arg(account_id)
    AND isn_id = (
        SELECT i.id
        FROM isn i
        JOIN isn_signal_types ist ON ist.isn_id = i.id
        JOIN signal_types st ON st.id = ist.signal_type_id
        WHERE i.slug = sqlc.arg(isn_slug)
            AND st.slug = sqlc.arg(slug)
            AND st.sem_ver = sqlc.arg(sem_ver)
            AND i.is_in_use = true
            AND ist.is_in_use = true
    )
    AND signal_type_id = (
        SELECT st.id
        FROM signal_types st
        WHERE st.slug = sqlc.arg(slug)
            AND st.sem_ver = sqlc.arg(sem_ver)
    )
    AND local_ref = sqlc.arg(local_ref);

-- name: GetSignalsWithOptionalFilters :many
-- you must supply the isn_slug,signal_type_slug & sem_ver params - other filters are optional
-- signals for inactive isns or signal_types are not returned (is_in_use = false)
-- supply viewer_account_id to restrict the results to the signals that account can see (used for write-only accounts):
-- its own signals and the signals correlated to its own signals
-- updated_since returns the signals that were created, given a new version, recorrelated or withdrawn since that time
SELECT
 a.id AS account_id,
    a.account_type,
    COALESCE(u.email, si.client_contact_email) AS email,
    s.id as signal_id,
    s.local_ref,
    st.slug AS signal_type_slug,
    st.sem_ver,
    st.content_kind,
    s.created_at signal_created_at,
    s.updated_at signal_updated_at,
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
-- the signal this signal is correlated to (uncorrelated signals are correlated to themselves)
JOIN
    signals c ON c.id = s.correlation_id
JOIN
    accounts a ON a.id = s.account_id
JOIN
    signal_types st on st.id = s.signal_type_id
join
    isn_signal_types ist ON ist.signal_type_id = st.id
JOIN
    isn i ON i.id = ist.isn_id
LEFT OUTER JOIN
    users u ON u.account_id = a.id
LEFT OUTER JOIN
    service_accounts si ON si.account_id = a.id
WHERE
    i.slug = sqlc.arg(isn_slug)
    AND s.isn_id = i.id
    AND st.slug = sqlc.arg(signal_type_slug)
    AND st.sem_ver = sqlc.arg(sem_ver)
    AND i.is_in_use = true
    AND ist.is_in_use = true
    AND (sqlc.narg('include_withdrawn')::boolean = true OR s.is_withdrawn = false)
    AND (sqlc.narg('viewer_account_id')::uuid IS NULL OR s.account_id = sqlc.narg('viewer_account_id')::uuid OR c.account_id = sqlc.narg('viewer_account_id')::uuid)
    AND (sqlc.narg('account_id')::uuid IS NULL OR a.id = sqlc.narg('account_id')::uuid)
    AND (sqlc.narg('signal_id')::uuid IS NULL OR s.id = sqlc.narg('signal_id')::uuid)
    AND (sqlc.narg('local_ref')::text IS NULL OR s.local_ref = sqlc.narg('local_ref')::text)
    -- signals correlated to the supplied signal (excluding the signal itself)
    AND (sqlc.narg('correlation_id')::uuid IS NULL OR (s.correlation_id = sqlc.narg('correlation_id')::uuid AND s.id != sqlc.narg('correlation_id')::uuid))
    AND (sqlc.narg('start_date')::timestamptz IS NULL OR lsv.created_at >= sqlc.narg('start_date')::timestamptz)
    AND (sqlc.narg('end_date')::timestamptz IS NULL OR lsv.created_at <= sqlc.narg('end_date')::timestamptz)
    AND (sqlc.narg('updated_since')::timestamptz IS NULL OR s.updated_at >= sqlc.narg('updated_since')::timestamptz)
ORDER BY
    s.updated_at ASC;

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
    JOIN isn_signal_types ist ON ist.signal_type_id = st.id
    JOIN isn i ON i.id = ist.isn_id
    WHERE s.id = sqlc.arg(correlation_id)
        AND i.slug = sqlc.arg(isn_slug)
        AND s.isn_id = i.id
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
-- supply viewer_account_id to restrict the results to the signals that account can see (used for write-only accounts):
-- its own signals, and all the signals correlated to its own signals
SELECT
    a.id AS account_id,
    a.account_type,
    COALESCE(u.email, si.client_contact_email) AS email,
    s.id as signal_id,
    s.local_ref,
    st.slug AS signal_type_slug,
    st.sem_ver,
    st.content_kind,
    s.created_at signal_created_at,
    s.updated_at signal_updated_at,
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
-- the signal this signal is correlated to
JOIN
    signals c ON c.id = s.correlation_id
JOIN
    accounts a ON a.id = s.account_id
JOIN
    signal_types st on st.id = s.signal_type_id
JOIN
    isn_signal_types ist ON ist.signal_type_id = st.id
JOIN
    isn i ON i.id = ist.isn_id
LEFT OUTER JOIN
    users u ON u.account_id = a.id
LEFT OUTER JOIN
    service_accounts si ON si.account_id = a.id
WHERE
    s.correlation_id = ANY(sqlc.slice(correlation_ids))
    AND s.correlation_id != s.id  -- exclude self-referencing signals
    AND s.isn_id = i.id
    AND i.is_in_use = true
    AND ist.is_in_use = true
    AND (sqlc.narg('include_withdrawn')::boolean = true OR s.is_withdrawn = false)
    AND (sqlc.narg('viewer_account_id')::uuid IS NULL OR s.account_id = sqlc.narg('viewer_account_id')::uuid OR c.account_id = sqlc.narg('viewer_account_id')::uuid)
ORDER BY
    s.correlation_id,
    s.local_ref,
    lsv.version_number,
    lsv.id;

-- name: GetIsnBySignalID :one
-- Returns the ISN id and slug for the ISN that owns the signal with the given ID.
SELECT i.id, i.slug
FROM signals s
JOIN isn i ON i.id = s.isn_id
WHERE s.id = $1
AND i.is_in_use = true;

-- name: GetPreviousSignalVersions :many
-- get the previous versions for the supplied signals (no rows returned if the signal only has 1 version)
SELECT sv.signal_id, id as signal_version_id, sv.created_at, sv.version_number, sv.content
FROM signal_versions  sv
WHERE
    sv.signal_id = ANY(sqlc.slice(signal_ids))
    -- exclude the latest version 
    AND sv.id != (SELECT id from latest_signal_versions lsv WHERE lsv.signal_id = sv.signal_id)
    ORDER BY sv.created_at;

-- name: GetLatestSignalVersionByLocalRef :one
-- returns the latest version of the account's signal with the supplied local_ref (used to detect unchanged document uploads)
SELECT
    s.id AS signal_id,
    s.correlation_id,
    s.is_withdrawn,
    lsv.id AS signal_version_id,
    lsv.version_number,
    lsv.content
FROM signals s
JOIN signal_types st ON st.id = s.signal_type_id
JOIN latest_signal_versions lsv ON lsv.signal_id = s.id
WHERE s.account_id = sqlc.arg(account_id)
    AND st.slug = sqlc.arg(signal_type_slug)
    AND st.sem_ver = sqlc.arg(sem_ver)
    AND s.local_ref = sqlc.arg(local_ref);

-- name: GetSignalVersion :one
-- returns a version of a signal on the ISN (the latest version if version_number is null)
-- correlated_to_account_id is the account that created the signal this signal is correlated to (used to check what write-only accounts can see)
SELECT
    s.account_id,
    c.account_id AS correlated_to_account_id,
    s.is_withdrawn,
    sv.version_number,
    sv.content
FROM signals s
JOIN signals c ON c.id = s.correlation_id
JOIN isn i ON i.id = s.isn_id
JOIN signal_types st ON st.id = s.signal_type_id
JOIN signal_versions sv ON sv.signal_id = s.id
    AND sv.account_id = s.account_id
WHERE s.id = sqlc.arg(signal_id)
    AND i.slug = sqlc.arg(isn_slug)
    AND st.slug = sqlc.arg(signal_type_slug)
    AND st.sem_ver = sqlc.arg(sem_ver)
    AND (sqlc.narg(version_number)::integer IS NULL OR sv.version_number = sqlc.narg(version_number)::integer)
ORDER BY sv.version_number DESC
LIMIT 1;
