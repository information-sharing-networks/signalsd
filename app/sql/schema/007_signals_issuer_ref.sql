-- +goose Up

-- issuer_ref is the reference given by the organization that issued the thing a signal represents (e.g. the certificate
-- number of an export health certificate). Like local_ref and correlation_id it identifies the signal, so it is stored on
-- signals (each submission sets it, so a resubmission without one clears it).
-- It is currently only supported for documents (json and event signals always have a null issuer_ref).
--
-- Recipients find documents with the issuer_ref search filter. issuer_ref is not unique, and most signals don't have one,
-- so the index only covers the signals that do.
--
-- signals is partitioned, so the index is created on every partition (and on partitions added later).
ALTER TABLE signals ADD COLUMN issuer_ref TEXT;

CREATE INDEX idx_signals_issuer_ref ON signals (issuer_ref) WHERE issuer_ref IS NOT NULL;

-- +goose Down

DROP INDEX idx_signals_issuer_ref;

ALTER TABLE signals DROP COLUMN issuer_ref;
