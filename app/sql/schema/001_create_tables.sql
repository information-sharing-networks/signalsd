-- +goose Up
CREATE TABLE users (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    email TEXT NOT NULL UNIQUE,
    hashed_password TEXT DEFAULT 'unset' NOT NULL
);

CREATE TABLE isn (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_id UUID NOT NULL,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    detail TEXT NOT NULL,
    is_in_use bool DEFAULT true NOT NULL,
    visibility TEXT NOT NULL,
    storage_type TEXT NOT NULL, 
    CONSTRAINT visibility_check
    CHECK (visibility IN ('public','private')),
    CONSTRAINT unique_isn_slug UNIQUE (slug),
    CONSTRAINT fk_isn_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE);


CREATE TABLE signal_defs (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_id UUID NOT NULL, 
    isn_id UUID NOT NULL,
    slug TEXT NOT NULL,
    schema_url TEXT NOT NULL,
    readme_url TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    sem_ver TEXT NOT NULL,
    stage TEXT NOT NULL,
    CONSTRAINT unique_signal_defs UNIQUE (slug, sem_ver),
    CONSTRAINT valid_signal_defs_slug_format
        CHECK (slug ~ '^[a-z0-9-]+$'),
    CONSTRAINT valid_schma_json_url 
        CHECK (schema_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.json$'),
    CONSTRAINT valid_readme_url 
        CHECK (readme_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.md$'),
    CONSTRAINT stage_check
    CHECK (stage IN ('dev','test', 'live', 'deprecated', 'closed','shuttered')),
    CONSTRAINT fk_signal_defs_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_signal_defs_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE);

CREATE TABLE refresh_tokens (
    token text NOT NULL PRIMARY KEY,
    user_id UUID NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_referesh_token_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE);


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


CREATE TABLE isn_retrievers (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_id UUID NOT NULL,
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
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE);

-- +goose Down

DROP table IF EXISTS isn_receivers CASCADE;
DROP table IF EXISTS isn_retrievers CASCADE;
DROP table IF EXISTS signal_defs CASCADE;
DROP table IF EXISTS refresh_tokens CASCADE;
DROP table IF EXISTS isn CASCADE;
DROP table IF EXISTS users CASCADE;