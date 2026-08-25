// Command kubby-backup exports and restores Kubby's own configuration.
//
// What it carries is what would take longest to rebuild by hand: the registered clusters
// and their kubeconfigs, the users and their roles, the grants between them, and the
// deployment settings. Not the audit trail — that belongs where it was shipped, and a
// backup that could rewrite it would be the wrong kind of tool.
//
// The archive is encrypted with a passphrase rather than with Kubby's own key. Restoring
// into a fresh installation is the case that matters, and that installation has a
// different key by definition; an export only its own instance could open would be
// useless exactly when it is needed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/erolbeyaz/kubby/internal/backup"
	"github.com/erolbeyaz/kubby/internal/config"
	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kubby-backup: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		export  = flag.String("export", "", "write an encrypted archive to this path")
		restore = flag.String("restore", "", "read an encrypted archive from this path")
		dryRun  = flag.Bool("dry-run", false, "with -restore, report what would change and write nothing")
	)
	flag.Parse()

	if (*export == "") == (*restore == "") {
		return errors.New("give exactly one of -export or -restore")
	}

	// The passphrase comes from the environment, never from a flag: a flag is in the
	// process list for anything else on the machine to read.
	passphrase := os.Getenv("KUBBY_BACKUP_PASSPHRASE")
	if passphrase == "" {
		return errors.New("set KUBBY_BACKUP_PASSPHRASE to the passphrase protecting the archive")
	}
	if len(passphrase) < backup.MinPassphrase {
		return fmt.Errorf("the passphrase must be at least %d characters: it is the only thing "+
			"between this archive and every cluster in it", backup.MinPassphrase)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	db, err := store.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer db.Close()

	keyring, err := crypto.NewKeyring(cfg.Crypto.EncryptionKeyVersion, cfg.Crypto.EncryptionKey)
	if err != nil {
		return err
	}

	service := backup.New(db, keyring)

	if *export != "" {
		summary, err := service.Export(ctx, *export, passphrase)
		if err != nil {
			return err
		}
		fmt.Printf("Wrote %s\n", *export)
		fmt.Printf("  %d clusters, %d users, %d grants, %d settings\n",
			summary.Clusters, summary.Users, summary.Grants, summary.Settings)
		fmt.Println()
		fmt.Println("The passphrase is not stored anywhere. Without it this archive cannot be opened.")
		return nil
	}

	summary, err := service.Restore(ctx, *restore, passphrase, *dryRun)
	if err != nil {
		return err
	}

	if *dryRun {
		fmt.Println("Dry run — nothing was written.")
	}
	fmt.Printf("  %d clusters, %d users, %d grants, %d settings\n",
		summary.Clusters, summary.Users, summary.Grants, summary.Settings)
	if summary.Skipped > 0 {
		fmt.Printf("  %d entries already existed and were left alone\n", summary.Skipped)
	}
	return nil
}
