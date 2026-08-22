// Command kubby-rotate-key rewraps every stored secret under a new encryption key.
//
// Envelope encryption means only the wrapped data keys change, so rotation touches each
// record once and needs no downtime (ADR-009). It is a separate command rather than a
// server endpoint because it is an operator action, not something the application
// should be able to trigger on its own.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/config"
	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/store"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "report what would be rewrapped without writing anything")
	flag.Parse()

	if err := run(*dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "kubby-rotate-key: %v\n", err)
		if errors.Is(err, errUndecryptable) {
			fmt.Fprintln(os.Stderr, undecryptableHelp)
		}
		os.Exit(1)
	}
}

func run(dryRun bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	previous, err := previousKey()
	if err != nil {
		return err
	}

	keyring, err := crypto.NewKeyring(cfg.Crypto.EncryptionKeyVersion, cfg.Crypto.EncryptionKey)
	if err != nil {
		return err
	}
	// The retired key stays available for decryption only, which is what lets records
	// written under it be read and rewrapped one at a time.
	if err := keyring.AddRetiredKey(cfg.Crypto.EncryptionKeyVersion-1, previous); err != nil {
		return fmt.Errorf("register the previous key: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	db, err := store.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer db.Close()

	fmt.Printf("Rotating to key version %d", cfg.Crypto.EncryptionKeyVersion)
	if dryRun {
		fmt.Print("  (dry run — nothing will be written)")
	}
	fmt.Println()

	clusters, err := rewrapClusterCredentials(ctx, db, keyring, dryRun)
	if err != nil {
		return err
	}
	secrets, err := rewrapTOTPSecrets(ctx, db, keyring, dryRun)
	if err != nil {
		return err
	}

	fmt.Printf("\nCluster credentials rewrapped: %d\nTOTP secrets rewrapped:       %d\n", clusters, secrets)
	if dryRun {
		fmt.Println("\nNothing was written. Re-run without -dry-run to apply.")
	}
	return nil
}

// errUndecryptable marks the one failure an operator is likely to hit, so main can
// print guidance without stuffing it into an error string.
var errUndecryptable = errors.New("record could not be decrypted with the available keys")

// undecryptable stops the run. Rotation aborts rather than skipping: a half-rotated
// store where some records are silently unreadable is much harder to reason about than
// one that has not moved.
func undecryptable(kind string, id uuid.UUID, err error) error {
	return fmt.Errorf("%w: %s for %s: %v", errUndecryptable, kind, id, err)
}

// undecryptableHelp is the operator-facing explanation for that failure.
const undecryptableHelp = `
This record was sealed with a key that is not in the keyring. Check that
KUBBY_ENCRYPTION_KEY_PREVIOUS is the key the running server currently uses, and that
KUBBY_ENCRYPTION_KEY is the new one.

Nothing was changed for this record. Records processed earlier in this run were already
rewrapped, and re-running after fixing the keys will skip them.`

// previousKey reads the retired key. Rotation is impossible without it: records sealed
// under the old key could not be opened, and rewrapping them would be guesswork.
func previousKey() ([]byte, error) {
	raw := os.Getenv("KUBBY_ENCRYPTION_KEY_PREVIOUS")
	if raw == "" {
		return nil, errors.New(
			"KUBBY_ENCRYPTION_KEY_PREVIOUS is required: set it to the key currently in use, " +
				"and KUBBY_ENCRYPTION_KEY to the new one")
	}

	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("KUBBY_ENCRYPTION_KEY_PREVIOUS must be standard base64: %w", err)
	}
	return key, nil
}

func rewrapClusterCredentials(ctx context.Context, db *store.DB, keyring *crypto.Keyring, dryRun bool) (int, error) {
	rows, err := db.Pool().Query(ctx, `SELECT cluster_id, kubeconfig_enc FROM cluster_credentials`)
	if err != nil {
		return 0, fmt.Errorf("list cluster credentials: %w", err)
	}

	type record struct {
		id   uuid.UUID
		blob []byte
	}
	var pending []record

	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.blob); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan cluster credential: %w", err)
		}
		if keyring.NeedsRewrap(r.blob) {
			pending = append(pending, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	count := 0
	for _, r := range pending {
		// The ciphertext is bound to its row, so the same identifier has to be used
		// again when resealing or the record becomes unreadable.
		aad := []byte(r.id.String())

		plaintext, err := keyring.Open(r.blob, aad)
		if err != nil {
			return count, undecryptable("cluster credential", r.id, err)
		}
		resealed, err := keyring.Seal(plaintext, aad)
		if err != nil {
			return count, fmt.Errorf("reseal credential for cluster %s: %w", r.id, err)
		}

		fmt.Printf("  cluster %s\n", r.id)
		if !dryRun {
			_, err = db.Pool().Exec(ctx,
				`UPDATE cluster_credentials SET kubeconfig_enc = $2, rotated_at = now() WHERE cluster_id = $1`,
				r.id, resealed)
			if err != nil {
				return count, fmt.Errorf("store rewrapped credential for cluster %s: %w", r.id, err)
			}
		}
		count++
	}
	return count, nil
}

func rewrapTOTPSecrets(ctx context.Context, db *store.DB, keyring *crypto.Keyring, dryRun bool) (int, error) {
	rows, err := db.Pool().Query(ctx,
		`SELECT id, totp_secret_enc FROM users WHERE totp_secret_enc IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("list TOTP secrets: %w", err)
	}

	type record struct {
		id   uuid.UUID
		blob []byte
	}
	var pending []record

	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.blob); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan TOTP secret: %w", err)
		}
		if keyring.NeedsRewrap(r.blob) {
			pending = append(pending, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	count := 0
	for _, r := range pending {
		aad := []byte(r.id.String())

		plaintext, err := keyring.Open(r.blob, aad)
		if err != nil {
			return count, undecryptable("TOTP secret", r.id, err)
		}
		resealed, err := keyring.Seal(plaintext, aad)
		if err != nil {
			return count, fmt.Errorf("reseal TOTP secret for user %s: %w", r.id, err)
		}

		fmt.Printf("  user %s\n", r.id)
		if !dryRun {
			if _, err := db.Pool().Exec(ctx,
				`UPDATE users SET totp_secret_enc = $2 WHERE id = $1`, r.id, resealed); err != nil {
				return count, fmt.Errorf("store rewrapped TOTP secret for user %s: %w", r.id, err)
			}
		}
		count++
	}
	return count, nil
}
