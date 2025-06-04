-- +goose Up 

-- correlation_ids can be of different types, but must be in the same isn.
DELETE FROM signals;
ALTER TABLE signals ADD COLUMN isn_id UUID NOT NULL;
ALTER TABLE signals DROP CONSTRAINT fk_correlation_id;
ALTER TABLE signals DROP CONSTRAINT unique_signals_signal_type_id_correlation_id;
ALTER TABLE signals ADD CONSTRAINT unique_signals_isn_id_signal_id UNIQUE (isn_id, id);
ALTER TABLE signals ADD CONSTRAINT fk_correlation_id FOREIGN KEY (isn_id,correlation_id)
    REFERENCES signals(isn_id,id);
ALTER TABLE signals ADD CONSTRAINT fk_signal_isn FOREIGN KEY (isn_id)
    REFERENCES isn(id)
    ON DELETE CASCADE;

-- +goose down
ALTER TABLE signals DROP COLUMN isn_id;
ALTER TABLE signals ADD CONSTRAINT unique_signals_signal_type_id_correlation_id UNIQUE (signal_type_id, id);
ALTER TABLE signals ADD CONSTRAINT fk_correlation_id FOREIGN KEY (signal_type_id,correlation_id)
    REFERENCES signals(signal_type_id,id);