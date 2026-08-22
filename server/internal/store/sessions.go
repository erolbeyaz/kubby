package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ip_address is read through host() because pgx cannot scan inet into a string
// directly; the column stays inet so Postgres still validates what goes in.
const sessionColumns = `
	id, user_id, host(ip_address), user_agent, mfa_satisfied,
	created_at, last_seen_at, expires_at, revoked_at`

type SessionRepo struct{ db *DB }

func (db *DB) Sessions() *SessionRepo { return &SessionRepo{db: db} }

// Create stores a new session. Only the token hash is persisted, so a database dump
// does not yield usable sessions.
func (r *SessionRepo) Create(ctx context.Context, userID uuid.UUID, tokenHash string, ip *netip.Addr, userAgent string, mfaSatisfied bool, expiresAt time.Time) (*Session, error) {
	row := r.db.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, ip_address, user_agent, mfa_satisfied, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+sessionColumns,
		userID, tokenHash, ipToText(ip), truncate(userAgent, 512), mfaSatisfied, expiresAt)

	session, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

// ByTokenHash finds a session by the hash of its token.
func (r *SessionRepo) ByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	row := r.db.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE refresh_token_hash = $1`, tokenHash)

	session, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}
	return session, nil
}

// Rotate atomically revokes the presented session and issues its successor.
//
// Rotation is a single transaction so a stolen refresh token cannot be replayed: the
// first use invalidates it, and a second use finds nothing to rotate.
func (r *SessionRepo) Rotate(ctx context.Context, oldTokenHash, newTokenHash string, expiresAt time.Time) (*Session, error) {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		userID       uuid.UUID
		ipText       *string
		userAgent    string
		mfaSatisfied bool
	)
	err = tx.QueryRow(ctx, `
		UPDATE sessions
		SET revoked_at = now()
		WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING user_id, host(ip_address), user_agent, mfa_satisfied`,
		oldTokenHash).Scan(&userID, &ipText, &userAgent, &mfaSatisfied)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("revoke rotated session: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, ip_address, user_agent, mfa_satisfied, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+sessionColumns,
		userID, newTokenHash, ipText, userAgent, mfaSatisfied, expiresAt)

	session, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("create rotated session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit rotation: %w", err)
	}
	return session, nil
}

func (r *SessionRepo) Touch(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (r *SessionRepo) MarkMFASatisfied(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.pool.Exec(ctx, `UPDATE sessions SET mfa_satisfied = TRUE WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark MFA satisfied: %w", err)
	}
	return nil
}

func (r *SessionRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllForUser ends every session, optionally sparing the caller's own.
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID, except *uuid.UUID) (int64, error) {
	query := `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
	args := []any{userID}
	if except != nil {
		query += ` AND id <> $2`
		args = append(args, *except)
	}

	tag, err := r.db.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *SessionRepo) ListActiveForUser(ctx context.Context, userID uuid.UUID) ([]*Session, error) {
	rows, err := r.db.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM sessions
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		 ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// DeleteExpired removes sessions that expired long enough ago to be useless for audit.
func (r *SessionRepo) DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := r.db.pool.Exec(ctx,
		`DELETE FROM sessions WHERE expires_at < now() - $1::interval`, olderThan.String())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanSession(row scannable) (*Session, error) {
	var s Session
	var ipText *string
	err := row.Scan(&s.ID, &s.UserID, &ipText, &s.UserAgent, &s.MFASatisfied,
		&s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.RevokedAt)
	if err != nil {
		return nil, err
	}
	if ipText != nil {
		if addr, parseErr := netip.ParseAddr(*ipText); parseErr == nil {
			s.IPAddress = &addr
		}
	}
	return &s, nil
}

func ipToText(ip *netip.Addr) *string {
	if ip == nil || !ip.IsValid() {
		return nil
	}
	s := ip.String()
	return &s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
