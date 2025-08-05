-- name: CreateSignalBatch :one
INSERT INTO signal_batches (
    id,
    created_at,
    updated_at,
    isn_id,
    account_id,
    is_latest
) VALUES (
    gen_random_uuid(), 
    now(), 
    now(), 
    $1, 
    $2, 
    TRUE
)
RETURNING *;

-- name: CreateOrGetWebUserSignalBatch :one
WITH isn_record AS (
    SELECT id
    FROM isn
    WHERE isn.slug = $1
),
inserted AS (
    INSERT INTO signal_batches (
        id,
        created_at,
        updated_at,
        isn_id,
        account_id,
        is_latest
    )
    SELECT
        gen_random_uuid(),
        now(),
        now(),
        isn_record.id,
        $2, -- account_id
        TRUE
    FROM isn_record
    ON CONFLICT (account_id, isn_id) WHERE is_latest = TRUE
    DO NOTHING
    RETURNING id
)
SELECT id as batch_id FROM inserted
UNION ALL
SELECT sb.id as batch_id FROM signal_batches sb
JOIN isn ON sb.isn_id = isn.id
WHERE sb.account_id = $2 AND sb.is_latest = TRUE
  AND NOT EXISTS (SELECT 1 FROM inserted);


-- name: CloseISNSignalBatchByAccountID :execrows
UPDATE signal_batches 
SET is_latest = FALSE
WHERE isn_id = $1 and account_id = $2;

-- name: GetLatestIsnSignalBatchesByAccountID :many
SELECT sb.*, i.slug as isn_slug FROM signal_batches sb 
JOIN isn i
    ON sb.isn_id = i.id
WHERE account_id = $1
AND is_latest = TRUE;

-- name: GetLatestBatchByAccountAndIsnSlug :one
SELECT sb.*, i.slug as isn_slug FROM signal_batches sb
JOIN isn i
ON i.id = sb.isn_id
WHERE sb.account_id = $1
AND i.slug = $2
AND sb.is_latest = TRUE;

-- name: GetLatestSignalBatchByIsnSlugAndBatchID :one
SELECT sb.*, i.slug as isn_slug FROM signal_batches sb
JOIN isn i
ON i.id = sb.isn_id
WHERE i.slug = $1
AND sb.id = $2
AND sb.is_latest = TRUE;

-- name: GetSignalBatchByID :one
SELECT sb.*, i.slug as isn_slug FROM signal_batches sb
JOIN isn i 
ON i.id = sb.isn_id
WHERE sb.id = $1;


-- name: GetLoadedSignalsSummaryByBatchID :many
-- count of sucessfully loaded signals grouped by signal type (note that a local_ref can be submitted multiple times and only the latest version is counted).
--
-- where a signal has failed processing and has not subsequently been loaded again, it is not counted
-- Uses latest_signal_versions view to avoid ROW_NUMBER() window function
SELECT
    COUNT(*) as submitted_count,
    st.slug AS signal_type_slug,
    st.sem_ver AS signal_type_sem_ver
FROM
    signal_batches sb
JOIN
    signal_versions sv ON sv.signal_batch_id = sb.id
JOIN
    latest_signal_versions lsv ON lsv.signal_id = sv.signal_id AND lsv.id = sv.id
JOIN
    signals s ON s.id = lsv.signal_id
JOIN
    signal_types st on st.id = s.signal_type_id
WHERE
    sb.id = $1
    AND NOT EXISTS ( -- do not count signals that failed processing and have not been corrected yet
        SELECT 1 FROM signal_processing_failures spf
        WHERE spf.signal_batch_id = $1
            AND spf.local_ref = s.local_ref
            AND spf.signal_type_slug = st.slug
            AND spf.signal_type_sem_ver = st.sem_ver
            AND spf.created_at > lsv.created_at
        )
GROUP BY st.slug, st.sem_ver;

-- name: GetFailedSignalsByBatchID :many
-- failed local_refs that were not subsequently loaded
SELECT DISTINCT
    sb.id as batch_id,
    sb.created_at as batch_opened_at,
    sb.account_id,
    i.slug as isn_slug,
    sb.is_latest,
    spf.signal_type_slug,
    spf.signal_type_sem_ver,
    spf.local_ref,
    spf.error_code,
    spf.error_message
FROM signal_batches sb
JOIN isn i ON i.id = sb.isn_id
JOIN signal_processing_failures spf ON spf.signal_batch_id = sb.id
JOIN signal_types st ON st.slug = spf.signal_type_slug
    AND st.sem_ver = spf.signal_type_sem_ver
WHERE sb.id = $1
AND NOT EXISTS (
        SELECT 1 FROM signals s 
        JOIN signal_versions sv ON sv.signal_id = s.id
        WHERE s.local_ref = spf.local_ref
            AND s.signal_type_id = st.id
            AND sv.created_at > spf.created_at
    );

-- name: GetBatchesWithOptionalFilters :many
WITH RankedBatches AS (
    SELECT
        sb.id as batch_id,
        sb.created_at,
        sb.updated_at,
        sb.account_id,
        sb.is_latest,
        i.slug as isn_slug,
        ROW_NUMBER() OVER (PARTITION BY sb.account_id ORDER BY sb.created_at DESC) as rn
    FROM signal_batches sb
    JOIN isn i ON i.id = sb.isn_id
    WHERE i.slug = sqlc.arg(isn_slug)
        -- Account permission: users see own batches, owner role sees all
        AND (sb.account_id = sqlc.narg('requesting_account_id')::uuid
             OR sqlc.narg('is_admin')::boolean = true)
        AND (sqlc.narg('created_after')::timestamptz IS NULL OR sb.created_at >= sqlc.narg('created_after')::timestamptz)
        AND (sqlc.narg('created_before')::timestamptz IS NULL OR sb.created_at <= sqlc.narg('created_before')::timestamptz)
        -- Closed date filters (only apply to closed batches: is_latest = false)
        AND (sqlc.narg('closed_after')::timestamptz IS NULL OR (sb.is_latest = false AND sb.updated_at >= sqlc.narg('closed_after')::timestamptz))
        AND (sqlc.narg('closed_before')::timestamptz IS NULL OR (sb.is_latest = false AND sb.updated_at <= sqlc.narg('closed_before')::timestamptz))
)
SELECT
    batch_id,
    created_at,
    updated_at,
    account_id,
    is_latest,
    isn_slug
FROM RankedBatches
WHERE
    -- Apply latest & previous filtering only when latest=true
    (sqlc.narg('latest')::boolean IS NOT TRUE
     OR (sqlc.narg('latest')::boolean = true AND sqlc.narg('is_admin')::boolean = true AND rn = 1)
     OR (sqlc.narg('latest')::boolean = true AND sqlc.narg('is_admin')::boolean = false AND is_latest = true))
    AND
    (sqlc.narg('previous')::boolean IS NOT TRUE
     OR (sqlc.narg('previous')::boolean = true AND sqlc.narg('is_admin')::boolean = true AND rn = 2)
     OR (sqlc.narg('previous')::boolean = true AND sqlc.narg('is_admin')::boolean = false))
ORDER BY created_at DESC
LIMIT CASE
    WHEN sqlc.narg('latest')::boolean = true AND sqlc.narg('is_admin')::boolean = false THEN 1  
    WHEN sqlc.narg('previous')::boolean = true AND sqlc.narg('is_admin')::boolean = false THEN 1
    ELSE NULL  -- No limit for admin latest/previous (returns per account)
END
OFFSET CASE
    WHEN sqlc.narg('previous')::boolean = true AND sqlc.narg('is_admin')::boolean = false THEN 1  -- Skip the latest to get previous (members only)
    ELSE 0
END;
