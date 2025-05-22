
-- +goose Up
DROP TABLE IF EXISTS isn_retrievers;
CREATE TABLE isn_retrievers (
    isn_id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    default_rate_limit INT NOT NULL, -- todo
    retriever_status TEXT NOT NULL,
    listener_count INT NOT NULL,
    CONSTRAINT fk_isn_retrievers_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE);

-- +goose Down
DROP TABLE IF EXISTS isn_retrievers;
CREATE TABLE isn_retrievers (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_account_id UUID NOT NULL,
    isn_id UUID NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    slug TEXT NOT NULL,
    retriever_origin TEXT NOT NULL,
    retriever_status TEXT NOT NULL,
    default_rate_limit INT NOT NULL, -- todo
    CONSTRAINT fk_isn_retrievers_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_isn_retrievers_user
        FOREIGN KEY (user_account_id)
        REFERENCES users(id)
        ON DELETE CASCADE);
