-- Split single permission column into separate can_read and can_write boolean columns

-- +goose Up

ALTER TABLE isn_accounts ADD COLUMN can_read BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE isn_accounts ADD COLUMN can_write BOOLEAN NOT NULL DEFAULT false;

-- set existing permissions to both read and write
UPDATE isn_accounts SET can_read = true, can_write = true;

ALTER TABLE isn_accounts DROP CONSTRAINT isn_accounts_permission_check;
ALTER TABLE isn_accounts DROP COLUMN permission;

-- +goose Down

ALTER TABLE isn_accounts ADD COLUMN permission TEXT;

UPDATE isn_accounts SET permission = 'write' WHERE can_read AND can_write;
UPDATE isn_accounts SET permission = 'read' WHERE can_read AND NOT can_write;
UPDATE isn_accounts SET permission = 'write' WHERE NOT can_read AND can_write;

ALTER TABLE isn_accounts ALTER COLUMN permission SET NOT NULL;
ALTER TABLE isn_accounts ADD CONSTRAINT isn_accounts_permission_check
    CHECK (permission IN ('read','write'));

ALTER TABLE isn_accounts DROP COLUMN can_read;
ALTER TABLE isn_accounts DROP COLUMN can_write;

