-- +goose Up
-- +goose StatementBegin

-- gen_random_uuid() comes from pgcrypto; every table uses UUID primary keys.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- System settings that are safe to store in clear text. Secrets never live here;
-- they belong in cluster_credentials with envelope encryption (ADR-009).
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE app_settings IS 'Non-secret application settings. Never store credentials here.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS app_settings;
-- +goose StatementEnd
