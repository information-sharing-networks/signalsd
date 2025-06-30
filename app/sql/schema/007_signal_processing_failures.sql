-- +goose Up

DROP TABLE IF EXISTS signal_processing_failures;

-- New table for individual signal failure records
CREATE TABLE signal_processing_failures (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    signal_batch_id UUID NOT NULL,
    signal_type_slug TEXT NOT NULL,
    signal_type_sem_ver TEXT NOT NULL,
    local_ref TEXT NOT NULL,
    error_code TEXT NOT NULL,
    error_message TEXT NOT NULL,

    CONSTRAINT fk_signal_processing_failures_batch FOREIGN KEY (signal_batch_id)
        REFERENCES signal_batches(id) ON DELETE CASCADE
);

CREATE INDEX idx_signal_processing_failures_batch_id
    ON signal_processing_failures(signal_batch_id);

CREATE INDEX idx_signal_processing_failures_error_code
    ON signal_processing_failures(error_code);

CREATE INDEX idx_signal_processing_failures_batch_local_ref
    ON signal_processing_failures(signal_batch_id, local_ref);

-- +goose Down
DROP TABLE IF EXISTS signal_processing_failures;
