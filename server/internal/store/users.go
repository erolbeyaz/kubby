package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/erolbeyaz/kubby/internal/rbac"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrEmailInUse    = errors.New("email is already registered")
	ErrSetupComplete = errors.New("setup has already been completed")
)

const userColumns = `
	id, email, display_name, provider, external_id, COALESCE(password_hash, ''),
	role, totp_secret_enc, totp_confirmed_at, is_active, failed_login_count,
	locked_until, lockout_count, blocked_at, last_login_at, created_at, updated_at`

type UserRepo struct{ db *DB }

func (db *DB) Users() *UserRepo { return &UserRepo{db: db} }

// CountUsers reports how many accounts exist. The setup wizard is only reachable while
// this is zero.
func (r *UserRepo) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.db.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CreateFirstAdmin inserts the initial administrator, but only if no user exists yet.
// The guard is a conditional INSERT rather than a check-then-insert, so two concurrent
// setup requests cannot both create an admin.
func (r *UserRepo) CreateFirstAdmin(ctx context.Context, email, displayName, passwordHash string) (*User, error) {
	row := r.db.pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, provider, password_hash, role)
		SELECT $1, $2, 'local', $3, 'admin'
		WHERE NOT EXISTS (SELECT 1 FROM users)
		RETURNING `+userColumns,
		strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash)

	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSetupComplete
	}
	if err != nil {
		return nil, fmt.Errorf("create first admin: %w", err)
	}
	return user, nil
}

func (r *UserRepo) Create(ctx context.Context, email, displayName, passwordHash string, role rbac.Role) (*User, error) {
	row := r.db.pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, provider, password_hash, role)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING `+userColumns,
		strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash, string(role))

	user, err := scanUser(row)
	if isUniqueViolation(err) {
		return nil, ErrEmailInUse
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (r *UserRepo) ByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row := r.db.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	return scanUserOrNotFound(row, "find user by id")
}

// ByEmail looks up a local account. Email comparison is case-insensitive.
func (r *UserRepo) ByEmail(ctx context.Context, email string) (*User, error) {
	row := r.db.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE provider = 'local' AND lower(email) = lower($1)`,
		strings.TrimSpace(email))
	return scanUserOrNotFound(row, "find user by email")
}

func (r *UserRepo) List(ctx context.Context) ([]*User, error) {
	rows, err := r.db.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// RecordFailedLogin increments the counter and, on reaching the threshold, starts a
// lockout whose length escalates with each previous lockout. After maxLockouts the
// account is blocked outright — unless it is an administrator, which is never blocked
// so an installation cannot lock itself out of its own administration.
//
// Everything happens in one statement so concurrent attempts cannot race past the limit.
func (r *UserRepo) RecordFailedLogin(ctx context.Context, id uuid.UUID, maxAttempts int, lockFor time.Duration, maxLockouts int) (*User, error) {
	row := r.db.pool.QueryRow(ctx, `
		UPDATE users
		SET failed_login_count = CASE
		        WHEN failed_login_count + 1 >= $2 THEN 0
		        ELSE failed_login_count + 1
		    END,
		    lockout_count = CASE
		        WHEN failed_login_count + 1 >= $2 THEN lockout_count + 1
		        ELSE lockout_count
		    END,
		    locked_until = CASE
		        WHEN failed_login_count + 1 >= $2 THEN now() + $3::interval
		        ELSE locked_until
		    END,
		    blocked_at = CASE
		        WHEN failed_login_count + 1 >= $2
		         AND lockout_count + 1 >= $4
		         AND role <> 'admin'
		         AND blocked_at IS NULL
		        THEN now()
		        ELSE blocked_at
		    END,
		    updated_at = now()
		WHERE id = $1
		RETURNING `+userColumns,
		id, maxAttempts, lockFor.String(), maxLockouts)
	return scanUserOrNotFound(row, "record failed login")
}

// Unblock lifts a block and clears the escalation state. Administrator action only.
func (r *UserRepo) Unblock(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE users
		SET blocked_at = NULL, locked_until = NULL, failed_login_count = 0,
		    lockout_count = 0, updated_at = now()
		WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("unblock user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordSuccessfulLogin clears the lockout state.
func (r *UserRepo) RecordSuccessfulLogin(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.pool.Exec(ctx, `
		UPDATE users
		SET failed_login_count = 0, lockout_count = 0, locked_until = NULL,
		    last_login_at = now(), updated_at = now()
		WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("record successful login: %w", err)
	}
	return nil
}

func (r *UserRepo) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, passwordHash)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) UpdateRole(ctx context.Context, id uuid.UUID, role rbac.Role) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE users SET role = $2, updated_at = now() WHERE id = $1`, id, string(role))
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE users SET is_active = $2, updated_at = now() WHERE id = $1`, id, active)
	if err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActiveAdmins guards against removing the last administrator.
func (r *UserRepo) CountActiveAdmins(ctx context.Context, excluding uuid.UUID) (int, error) {
	var n int
	err := r.db.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin' AND is_active AND id <> $1`, excluding).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count active admins: %w", err)
	}
	return n, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type scannable interface {
	Scan(dest ...any) error
}

func scanUser(row scannable) (*User, error) {
	var u User
	var role string
	err := row.Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Provider, &u.ExternalID, &u.PasswordHash,
		&role, &u.TOTPSecretEnc, &u.TOTPConfirmedAt, &u.IsActive, &u.FailedLoginCount,
		&u.LockedUntil, &u.LockoutCount, &u.BlockedAt, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.Role = rbac.Role(role)
	return &u, nil
}

func scanUserOrNotFound(row scannable, op string) (*User, error) {
	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return user, nil
}

// SetTOTPSecret stores an encrypted TOTP secret and clears any previous confirmation,
// so a re-enrolment must be proven again before MFA counts as active.
func (r *UserRepo) SetTOTPSecret(ctx context.Context, id uuid.UUID, secret []byte) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE users SET totp_secret_enc = $2, totp_confirmed_at = NULL, updated_at = now() WHERE id = $1`,
		id, secret)
	if err != nil {
		return fmt.Errorf("set TOTP secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) ConfirmTOTP(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE users SET totp_confirmed_at = now(), updated_at = now() WHERE id = $1 AND totp_secret_enc IS NOT NULL`,
		id)
	if err != nil {
		return fmt.Errorf("confirm TOTP: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DisableTOTP removes the second factor entirely.
func (r *UserRepo) DisableTOTP(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE users SET totp_secret_enc = NULL, totp_confirmed_at = NULL, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("disable TOTP: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RestoredUser is an account being brought back from an archive, with the identity it
// had before rather than a new one: the grants in the same archive refer to it by id.
type RestoredUser struct {
	ID           uuid.UUID
	Email        string
	DisplayName  string
	PasswordHash string
	Role         rbac.Role
	IsActive     bool
}

// RestoreUser writes an account back with its original id.
//
// Separate from Create because it is a different act: Create mints an identity, this one
// preserves one. Keeping them apart means the ordinary path cannot be handed an id by
// accident, which is how a caller ends up choosing user ids.
//
// Nothing is overwritten: a conflicting id or email leaves what is there alone, so a
// restore run against a live installation by mistake is a no-op rather than the last
// thing that happens to it.
func (r *UserRepo) RestoreUser(ctx context.Context, in RestoredUser) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, role, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING`,
		in.ID, in.Email, in.DisplayName, in.PasswordHash, string(in.Role), in.IsActive)
	if err != nil {
		return fmt.Errorf("restore user: %w", err)
	}
	return nil
}
