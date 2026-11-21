-- +goose Up

-- Tidy up redundant constraints/indexes on the partitioned signal_versions table,
-- and add account_id to the latest_signal_versions subquery so PostgreSQL can prune hash partitions.

-- Note: 001 also declares unique_signals_id_account_id UNIQUE (id, account_id) on signals, but PostgreSQL
-- silently skips a UNIQUE constraint in CREATE TABLE that duplicates the primary key, so it was never created.

-- signal_versions: unique_signal_versions_signal_id_version covers (signal_id, version_number, account_id).
-- The other three constraints/indexes are redundant with it:
-- - unique_signal_id_version_number enforces the same uniqueness with a different column order
--   (no query filters signal_versions by account_id without signal_id, and account_id already prunes partitions)
-- - idx_signal_versions_signal_id and idx_signal_versions_signal_id_version are leading prefixes of it
ALTER TABLE signal_versions
    DROP CONSTRAINT unique_signal_id_version_number;

DROP INDEX idx_signal_versions_signal_id;
DROP INDEX idx_signal_versions_signal_id_version;

-- signal_versions is hash-partitioned by account_id, and every version of a signal belongs to the
-- same account_id (enforced by fk_signal_version_signal_id). Including account_id in the correlated
-- subquery lets PostgreSQL prune the other 7 partitions when looking up the latest version.
CREATE OR REPLACE VIEW latest_signal_versions AS
    SELECT id, created_at, account_id, signal_batch_id, signal_id, version_number, content
    FROM signal_versions sv
    WHERE version_number = (
        SELECT max(sv2.version_number)
        FROM signal_versions sv2
        WHERE sv2.signal_id = sv.signal_id
            AND sv2.account_id = sv.account_id
    );

-- +goose Down

CREATE OR REPLACE VIEW latest_signal_versions AS
    SELECT id, created_at, account_id, signal_batch_id, signal_id, version_number, content
    FROM signal_versions sv
    WHERE version_number = (
        SELECT max(sv2.version_number)
        FROM signal_versions sv2
        WHERE sv2.signal_id = sv.signal_id
    );

CREATE INDEX idx_signal_versions_signal_id_version ON signal_versions (signal_id, version_number);
CREATE INDEX idx_signal_versions_signal_id ON signal_versions (signal_id);

ALTER TABLE signal_versions
    ADD CONSTRAINT unique_signal_id_version_number UNIQUE (account_id, signal_id, version_number);
