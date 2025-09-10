-- +goose Up

-- Make email uniqueness case-insensitive for users table
ALTER TABLE users DROP CONSTRAINT users_email_key;
CREATE UNIQUE INDEX users_email_unique_ci ON users (LOWER(email));

-- Make email/organization uniqueness case-insensitive for service_accounts table  
ALTER TABLE service_accounts DROP CONSTRAINT fk_service_account_unique;
CREATE UNIQUE INDEX service_accounts_email_org_unique_ci 
ON service_accounts (LOWER(client_contact_email), LOWER(client_organization));

-- +goose Down

DROP INDEX IF EXISTS users_email_unique_ci;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);

DROP INDEX IF EXISTS service_accounts_email_org_unique_ci;
ALTER TABLE service_accounts ADD CONSTRAINT fk_service_account_unique UNIQUE (client_contact_email, client_organization);
