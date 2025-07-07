-- +goose Up
-- remove redudnant account_type from signal_batches
ALTER TABLE signal_batches DROP COLUMN account_type;

-- +goose Down
ALTER TABLE signal_batches ADD COLUMN account_type TEXT NOT NULL;
UPDATE signal_batches SET account_type = (select account_type from accounts where accounts.id = signal_batches.account_id);
ALTER TABLE signal_batches ALTER COLUMN account_type SET NOT NULL;
ALTER TABLE signal_batches ADD CONSTRAINT signal_batches_account_type_check
    CHECK (account_type IN ('service_account','user'));