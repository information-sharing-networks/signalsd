-- rename signal_types to signal_types 
-- +goose Up
DROP TABLE IF EXISTS signal_types;
CREATE TABLE signal_types (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    isn_id UUID NOT NULL,
    slug TEXT NOT NULL,
    schema_url TEXT NOT NULL,
    readme_url TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    sem_ver TEXT NOT NULL,
    stage TEXT NOT NULL,
    CONSTRAINT unique_signal_types UNIQUE (slug, sem_ver),
    CONSTRAINT valid_signal_types_slug_format
        CHECK (slug ~ '^[a-z0-9-]+$'),
    CONSTRAINT valid_schma_json_url 
        CHECK (schema_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.json$'),
    CONSTRAINT valid_readme_url 
        CHECK (readme_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.md$'),
    CONSTRAINT stage_check
    CHECK (stage IN ('dev','test', 'live', 'deprecated', 'closed','shuttered')),
    CONSTRAINT fk_signal_types_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE);

-- +goose Down
DROP TABLE signal_types;
CREATE TABLE signal_types (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    user_account_id UUID NOT NULL, 
    isn_id UUID NOT NULL,
    slug TEXT NOT NULL,
    schema_url TEXT NOT NULL,
    readme_url TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    sem_ver TEXT NOT NULL,
    stage TEXT NOT NULL,
    CONSTRAINT unique_signal_types UNIQUE (slug, sem_ver),
    CONSTRAINT valid_signal_types_slug_format
        CHECK (slug ~ '^[a-z0-9-]+$'),
    CONSTRAINT valid_schma_json_url 
        CHECK (schema_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.json$'),
    CONSTRAINT valid_readme_url 
        CHECK (readme_url ~ '^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+.*\.md$'),
    CONSTRAINT stage_check
    CHECK (stage IN ('dev','test', 'live', 'deprecated', 'closed','shuttered')),
    CONSTRAINT fk_signal_types_isn
        FOREIGN KEY (isn_id)
        REFERENCES isn(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_signal_types_user
        FOREIGN KEY (user_account_id)
        REFERENCES users(id)
        ON DELETE CASCADE);