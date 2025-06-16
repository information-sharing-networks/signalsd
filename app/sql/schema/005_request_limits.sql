-- +goose Up
-- remove rate limit from service accounts (we are just using a simple global rate limiter for now) 
ALTER TABLE service_accounts DROP COLUMN rate_limit_per_minute;

-- +goose Down
ALTER TABLE service_accounts ADD COLUMN rate_limit_per_minute INT NOT NULL DEFAULT 100;
