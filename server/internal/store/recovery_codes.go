package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type RecoveryCodeRepo struct{ db *DB }

func (db *DB) RecoveryCodes() *RecoveryCodeRepo { return &RecoveryCodeRepo{db: db} }

// Replace swaps a user's recovery codes for a fresh set in one transaction, so a
// failure cannot leave the account with no usable codes.
func (r *RecoveryCodeRepo) Replace(ctx context.Context, userID uuid.UUID, hashes []string) error {
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin recovery code replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("clear recovery codes: %w", err)
	}
	for _, hash := range hashes {
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES ($1, $2)`, userID, hash); err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit recovery codes: %w", err)
	}
	return nil
}

// UnusedHashes returns the ids and hashes of codes that have not been consumed.
func (r *RecoveryCodeRepo) UnusedHashes(ctx context.Context, userID uuid.UUID) (ids []uuid.UUID, hashes []string, err error) {
	rows, err := r.db.pool.Query(ctx,
		`SELECT id, code_hash FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL ORDER BY created_at`,
		userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list recovery codes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, nil, fmt.Errorf("scan recovery code: %w", err)
		}
		ids = append(ids, id)
		hashes = append(hashes, hash)
	}
	return ids, hashes, rows.Err()
}

// Consume marks a code used. The conditional guard makes a code single-use even if two
// requests present it at the same time.
func (r *RecoveryCodeRepo) Consume(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.pool.Exec(ctx,
		`UPDATE user_recovery_codes SET used_at = now() WHERE id = $1 AND used_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("consume recovery code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *RecoveryCodeRepo) CountUnused(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.db.pool.QueryRow(ctx,
		`SELECT count(*) FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL`, userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return n, nil
}
