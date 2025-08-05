-- +goose Up

-- Create a view that returns the latest version of each signal
CREATE VIEW latest_signal_versions AS
SELECT 
    sv.*
FROM signal_versions sv
WHERE sv.version_number = (
    SELECT MAX(sv2.version_number)
    FROM signal_versions sv2
    WHERE sv2.signal_id = sv.signal_id
);

-- +goose Down

DROP VIEW IF EXISTS latest_signal_versions;
