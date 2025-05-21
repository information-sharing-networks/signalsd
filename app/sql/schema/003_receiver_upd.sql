-- simplified isn_receiver design
-- +goose Up
DROP TABLE IF EXISTS isn_receivers;
CREATE TABLE isn_receivers (
    isn_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    max_daily_validation_failures INT NOT NULL,
    max_payload_kilobytes INT NOT NULL,
    payload_validation TEXT NOT NULL,
    default_rate_limit INT NOT NULL, -- resquests per second
    receiver_status TEXT NOT NULL,
    listener_count INT NOT NULL,
    CONSTRAINT isn_receivers_validation_check
    CHECK (payload_validation IN ('always','never', 'optional')),
    CONSTRAINT fk_isn_receivers_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE);

-- +goose Down

DROP TABLE IF EXISTS isn_receivers;
CREATE TABLE isn_receivers (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_id UUID NOT NULL,
    isn_id UUID NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    slug TEXT NOT NULL,
    receiver_origin TEXT NOT NULL,
    min_batch_records INT NOT NULL,
    max_batch_records INT NOT NULL,
    max_daily_validation_failures INT NOT NULL,
    max_payload_kilobytes INT NOT NULL,
    payload_validation TEXT NOT NULL,
    default_rate_limit INT NOT NULL, -- resquests per second
    receiver_status TEXT NOT NULL,
    CONSTRAINT isn_receivers_validation_check
    CHECK (payload_validation IN ('always','never', 'optional')),
    CONSTRAINT fk_isn_receivers_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_isn_receivers_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE);