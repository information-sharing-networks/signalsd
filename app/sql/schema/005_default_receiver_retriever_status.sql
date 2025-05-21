-- +goose Up
ALTER TABLE isn_receivers
ALTER COLUMN receiver_status 
SET DEFAULT 'offline';

ALTER TABLE isn_retrievers
ALTER COLUMN retriever_status
SET DEFAULT 'offline';

-- +goose Down
ALTER TABLE isn_receivers
ALTER COLUMN receiver_status 
DROP DEFAULT;

ALTER TABLE isn_retrievers
ALTER COLUMN retriever_status
DROP DEFAULT;
