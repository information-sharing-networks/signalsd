
-- name: UpsertSignalBatch :one
INSERT INTO signal_batches (batch_ref, account_id)
VALUES ($1, $2)
ON CONFLICT (account_id, batch_ref) DO UPDATE
  SET batch_ref = EXCLUDED.batch_ref  -- no-op update to trigger RETURNING
RETURNING id, batch_ref, account_id, created_at;

-- name: GetSignalBatchByID :one
SELECT id, batch_ref, account_id, created_at FROM signal_batches
WHERE id = $1;

-- name: GetSignalBatchByRefAndAccountID :one
SELECT id, batch_ref, account_id, created_at FROM signal_batches
WHERE account_id = $1 AND batch_ref = $2;


-- name: GetLoadedSignalsSummaryByBatchID :many
-- Count successfully loaded signals grouped by ISN and signal type.
-- A local_ref submitted multiple times is counted once (latest version only).
-- Signals with an unresolved processing failure are excluded.
-- Uses latest_signal_versions view to avoid ROW_NUMBER() window function.
SELECT
    COUNT(*) as submitted_count,
    i.slug AS isn_slug,
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
JOIN
    isn i ON i.id = s.isn_id
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
GROUP BY i.slug, st.slug, st.sem_ver;

-- name: GetFailedSignalsByBatchID :many
-- Unresolved failures: failed local_refs that were not subsequently loaded successfully.
-- ISN slug is derived via signals to avoid depending on isn_id on signal_batches.
SELECT DISTINCT
    sb.id as batch_id,
    sb.created_at as batch_created_at,
    sb.account_id,
    i.slug as isn_slug,
    spf.signal_type_slug,
    spf.signal_type_sem_ver,
    spf.local_ref,
    spf.error_code,
    spf.error_message
FROM signal_batches sb
JOIN signal_processing_failures spf ON spf.signal_batch_id = sb.id
JOIN signal_types st ON st.slug = spf.signal_type_slug
    AND st.sem_ver = spf.signal_type_sem_ver
JOIN signals s ON s.local_ref = spf.local_ref
    AND s.signal_type_id = st.id
    AND s.account_id = sb.account_id
JOIN isn i ON i.id = s.isn_id
WHERE sb.id = $1
AND NOT EXISTS (
        SELECT 1 FROM signal_versions sv
        WHERE sv.signal_id = s.id
            AND sv.created_at > spf.created_at
    );

-- name: GetBatchesWithOptionalFilters :many
SELECT
    sb.id   AS batch_id,
    sb.batch_ref,
    sb.created_at,
    sb.account_id
FROM signal_batches sb
WHERE
    -- Access control: members see their own batches; site admins see all
    (sb.account_id = sqlc.narg('requesting_account_id')::uuid
     OR sqlc.narg('is_admin')::boolean = true)
    AND (sqlc.narg('created_after')::timestamptz IS NULL OR sb.created_at >= sqlc.narg('created_after')::timestamptz)
    AND (sqlc.narg('created_before')::timestamptz IS NULL OR sb.created_at <= sqlc.narg('created_before')::timestamptz)
ORDER BY sb.created_at DESC;
