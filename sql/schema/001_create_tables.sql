-- +goose Up
CREATE TABLE users (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    email TEXT NOT NULL UNIQUE,
    hashed_password TEXT DEFAULT 'unset' NOT NULL
);

CREATE TABLE signal_defs (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    slug TEXT NOT NULL,
    schema_url TEXT NOT NULL,
    readme_url TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    sem_ver TEXT NOT NULL,
    stage TEXT NOT NULL,
    user_id UUID NOT NULL, 
    CONSTRAINT unique_signal_defs UNIQUE (slug, sem_ver),
    CONSTRAINT valid_signal_defs_slug_format
        CHECK (slug ~ '^[a-z0-9-]+$'),
    CONSTRAINT valid_schma_json_url 
        CHECK (schema_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.json$'),
    CONSTRAINT valid_readme_url 
        CHECK (readme_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.md$'),
    CONSTRAINT stage_check
    CHECK (stage IN ('dev','test', 'live', 'deprecated', 'closed')),
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

-- +goose Down

DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS signal_defs;
DROP TABLE IF EXISTS users;

