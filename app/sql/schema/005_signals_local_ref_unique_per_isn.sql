-- +goose Up

-- local_ref is now unique per ISN: (account_id, isn_id, signal_type_id, local_ref).
--
-- Previously local_ref was unique per (account_id, signal_type_id, local_ref). Since a signal type can now be added to several ISNs,
-- a signal sent to ISN Alpha with a local_ref the account had already used in ISN Beta was stored as a new version of the alpha signal
-- (and could be correlated to a signal in beta), which broke ISN isolation.
-- Sending the same local_ref to two ISNs now creates two independent signals.
--
ALTER TABLE signals
    DROP CONSTRAINT unique_signals_account_signal_type_local_ref,
    ADD CONSTRAINT unique_signals_account_isn_signal_type_local_ref UNIQUE (account_id, isn_id, signal_type_id, local_ref);

-- +goose Down

-- note this will fails if an account has used the same local_ref for the same signal type in more than one ISN
-- SELECT s.id FROM signals s JOIN signals c ON c.id = s.correlation_id WHERE s.isn_id <> c.isn_id; to find conflicts (fix manually)
ALTER TABLE signals
    DROP CONSTRAINT unique_signals_account_isn_signal_type_local_ref,
    ADD CONSTRAINT unique_signals_account_signal_type_local_ref UNIQUE (account_id, signal_type_id, local_ref);
