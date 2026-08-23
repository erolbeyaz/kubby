package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SettingsRepo reads and writes deployment-wide settings.
type SettingsRepo struct {
	db *DB
}

// Settings returns the repository.
func (d *DB) Settings() *SettingsRepo { return &SettingsRepo{db: d} }

// Get reads one setting into the value pointed at.
//
// A missing setting is not an error: every setting has a default, and the absence of a
// row means nobody has moved it off that default yet.
func (r *SettingsRepo) Get(ctx context.Context, key string, into any) (found bool, err error) {
	var raw []byte
	err = r.db.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&raw)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read setting %q: %w", key, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("decode setting %q: %w", key, err)
	}
	return true, nil
}

// Put writes one setting.
func (r *SettingsRepo) Put(ctx context.Context, key string, value any, by uuid.UUID) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode setting %q: %w", key, err)
	}

	_, err = r.db.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`,
		key, raw, by)
	if err != nil {
		return fmt.Errorf("write setting %q: %w", key, err)
	}
	return nil
}

// GetSecret reads a sealed setting secret. The caller opens it; this layer never holds
// a plaintext credential.
func (r *SettingsRepo) GetSecret(ctx context.Context, key string) (ciphertext []byte, version int, found bool, err error) {
	err = r.db.pool.QueryRow(ctx,
		`SELECT ciphertext, key_version FROM setting_secrets WHERE key = $1`, key).
		Scan(&ciphertext, &version)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("read setting secret %q: %w", key, err)
	}
	return ciphertext, version, true, nil
}

// PutSecret stores a sealed setting secret.
func (r *SettingsRepo) PutSecret(ctx context.Context, key string, ciphertext []byte, version int, by uuid.UUID) error {
	_, err := r.db.pool.Exec(ctx, `
		INSERT INTO setting_secrets (key, ciphertext, key_version, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) DO UPDATE
		   SET ciphertext = EXCLUDED.ciphertext,
		       key_version = EXCLUDED.key_version,
		       updated_at = now(),
		       updated_by = EXCLUDED.updated_by`,
		key, ciphertext, version, by)
	if err != nil {
		return fmt.Errorf("write setting secret %q: %w", key, err)
	}
	return nil
}

// DeleteSecret removes a stored secret, for when a setting stops needing one.
func (r *SettingsRepo) DeleteSecret(ctx context.Context, key string) error {
	if _, err := r.db.pool.Exec(ctx, `DELETE FROM setting_secrets WHERE key = $1`, key); err != nil {
		return fmt.Errorf("delete setting secret %q: %w", key, err)
	}
	return nil
}
