-- +goose Up
-- +goose StatementBegin

-- Identity provider is abstract from the start so adding OIDC later needs no schema
-- migration (ADR-023). Local users carry provider='local' and a password hash.
CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT        NOT NULL,
    display_name        TEXT        NOT NULL DEFAULT '',
    provider            TEXT        NOT NULL DEFAULT 'local',
    external_id         TEXT,
    password_hash       TEXT,
    role                TEXT        NOT NULL,
    totp_secret_enc     BYTEA,
    totp_confirmed_at   TIMESTAMPTZ,
    is_active           BOOLEAN     NOT NULL DEFAULT TRUE,
    failed_login_count  INTEGER     NOT NULL DEFAULT 0,
    locked_until        TIMESTAMPTZ,
    last_login_at       TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT users_role_valid CHECK (role IN ('admin', 'user', 'readonly')),
    CONSTRAINT users_provider_valid CHECK (provider IN ('local', 'oidc')),
    CONSTRAINT users_local_has_password CHECK (provider <> 'local' OR password_hash IS NOT NULL)
);

-- Email is unique per provider: the same address may exist locally and via OIDC.
CREATE UNIQUE INDEX users_provider_email_key ON users (provider, lower(email));
CREATE UNIQUE INDEX users_provider_external_id_key ON users (provider, external_id)
    WHERE external_id IS NOT NULL;

COMMENT ON COLUMN users.totp_secret_enc IS 'TOTP secret, encrypted at rest. Never stored or logged in clear text.';

-- Recovery codes are single-use and stored hashed, like passwords.
CREATE TABLE user_recovery_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash   TEXT        NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX user_recovery_codes_user_idx ON user_recovery_codes (user_id) WHERE used_at IS NULL;

-- Sessions are server-side so they can be revoked. Only the token hash is stored;
-- possession of the database must not yield a usable token.
CREATE TABLE sessions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    refresh_token_hash TEXT       NOT NULL UNIQUE,
    ip_address        INET,
    user_agent        TEXT        NOT NULL DEFAULT '',
    mfa_satisfied     BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL,
    revoked_at        TIMESTAMPTZ
);

CREATE INDEX sessions_user_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx ON sessions (expires_at) WHERE revoked_at IS NULL;

-- The audit stream cannot be turned off and is always retained locally (ADR-010).
-- Append-only by convention: nothing in the application issues UPDATE or DELETE here.
CREATE TABLE audit_events (
    id             BIGSERIAL PRIMARY KEY,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id       UUID REFERENCES users (id) ON DELETE SET NULL,
    actor_email    TEXT        NOT NULL DEFAULT '',
    action         TEXT        NOT NULL,
    result         TEXT        NOT NULL,
    cluster_id     UUID,
    namespace      TEXT,
    resource_kind  TEXT,
    resource_name  TEXT,
    ip_address     INET,
    request_id     TEXT        NOT NULL DEFAULT '',
    details        JSONB       NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT audit_events_result_valid CHECK (result IN ('success', 'denied', 'error'))
);

CREATE INDEX audit_events_occurred_idx ON audit_events (occurred_at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_id, occurred_at DESC);
CREATE INDEX audit_events_action_idx ON audit_events (action, occurred_at DESC);
CREATE INDEX audit_events_details_idx ON audit_events USING GIN (details);

COMMENT ON TABLE audit_events IS 'Append-only. Never UPDATE or DELETE except by the retention job.';
COMMENT ON COLUMN audit_events.details IS 'Redacted before insert. Must never contain secrets.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS user_recovery_codes;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
