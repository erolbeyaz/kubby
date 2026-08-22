-- +goose Up
-- +goose StatementBegin

-- Lockouts escalate: each one lasts longer than the last, and after the configured
-- number of lockouts a non-admin account is blocked outright.
ALTER TABLE users
    ADD COLUMN lockout_count INTEGER     NOT NULL DEFAULT 0,
    ADD COLUMN blocked_at    TIMESTAMPTZ;

COMMENT ON COLUMN users.lockout_count IS 'Consecutive lockouts. Reset by a successful sign-in or an administrator.';
COMMENT ON COLUMN users.blocked_at IS 'Set when repeated lockouts block the account. Administrators are never blocked.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN IF EXISTS blocked_at;
ALTER TABLE users DROP COLUMN IF EXISTS lockout_count;
-- +goose StatementEnd
