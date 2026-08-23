-- +goose Up
-- +goose StatementBegin
-- Deployment-wide settings an admin edits at runtime.
--
-- One row per setting rather than one row of columns: the set grows every phase, and a
-- wide table means a migration for every new option. Values are JSON so a setting can be
-- a shape rather than a string.
CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       JSONB       NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  UUID        REFERENCES users (id) ON DELETE SET NULL
);

-- Secrets belonging to those settings — a Prometheus password, an Elasticsearch API key.
--
-- Kept apart from the settings themselves so the settings can be read, logged and
-- returned to the browser freely, and the ciphertext never travels with them by accident.
-- The blob is sealed by the same keyring as cluster credentials and bound to its key,
-- so a row copied to another key's name will not open.
CREATE TABLE setting_secrets (
    key         TEXT PRIMARY KEY,
    ciphertext  BYTEA       NOT NULL,
    key_version INTEGER     NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  UUID        REFERENCES users (id) ON DELETE SET NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE setting_secrets;
DROP TABLE settings;
-- +goose StatementEnd
