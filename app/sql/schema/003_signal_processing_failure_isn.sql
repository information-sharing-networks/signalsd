-- +goose Up

-- signal_processing_failures now records the ISN the signal was sent to.
--
-- Previously the batch status endpoint found the ISN of a failure via the signals table, so failures for signals that
-- were never stored (e.g. a first submission that failed validation) were not reported.
--
-- isn_slug is NULL for signals the signal router could not route to an ISN.
ALTER TABLE signal_processing_failures
    ADD COLUMN isn_slug TEXT;

-- backfill existing failures from the signals they relate to
UPDATE signal_processing_failures spf
SET isn_slug = i.slug
FROM signal_batches sb, signal_types st, signals s, isn i
WHERE sb.id = spf.signal_batch_id
    AND st.slug = spf.signal_type_slug
    AND st.sem_ver = spf.signal_type_sem_ver
    AND s.local_ref = spf.local_ref
    AND s.signal_type_id = st.id
    AND s.account_id = sb.account_id
    AND i.id = s.isn_id;

-- +goose Down

ALTER TABLE signal_processing_failures
    DROP COLUMN IF EXISTS isn_slug;
