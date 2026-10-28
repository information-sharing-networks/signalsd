-- +goose Up

-- Polling with updated_since (GET /api/signals/search?updated_since=...) searches every signal type in the caller's ISNs,
-- so it can't use idx_signals_isn_signal_type to narrow the search to one type. This index lets it read only the signals
-- that changed since the cursor.
--
-- signals is partitioned, so the index is created on every partition (and on partitions added later).
CREATE INDEX idx_signals_isn_updated_at ON signals (isn_id, updated_at);

-- +goose Down

DROP INDEX idx_signals_isn_updated_at;
