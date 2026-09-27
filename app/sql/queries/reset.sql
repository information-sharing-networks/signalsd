-- name: DeleteAccounts :execrows
DELETE FROM Accounts;

-- name: DeleteSignalTypes :execrows
-- signal types are not owned by an account, so they are not deleted by the DeleteAccounts cascade
DELETE FROM signal_types;
