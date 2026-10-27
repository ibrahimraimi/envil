-- +goose Up
CREATE TABLE IF NOT EXISTS accounts (
    id            BYTEA PRIMARY KEY,
    public_key    BYTEA NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    quota_bytes   BIGINT NOT NULL DEFAULT 5242880
);

CREATE TABLE IF NOT EXISTS projects (
    id            UUID PRIMARY KEY,
    account_id    BYTEA NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    meta_blob     BYTEA NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS backups (
    id            BIGSERIAL PRIMARY KEY,
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version       INT NOT NULL,
    blob          BYTEA NOT NULL,
    size_bytes    INT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, version)
);

CREATE INDEX IF NOT EXISTS idx_projects_account ON projects(account_id);
CREATE INDEX IF NOT EXISTS idx_backups_project ON backups(project_id, version DESC);

-- +goose Down
DROP TABLE IF EXISTS backups;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS accounts;
