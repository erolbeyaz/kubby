// Package storetest gives a test its own database schema.
//
// Every test that touches Postgres gets a schema of its own, created and dropped around
// it. Without that, a test run reaches into whatever database the DSN names — in
// development, the one the running Kubby is using — and rewrites its rows. That is not a
// hypothetical: a cluster test sealing credentials with its own key left the developer's
// real cluster reporting "ciphertext is malformed", which looks like corruption and is
// nothing of the kind.
package storetest

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/store"
)

// DSNEnv names the connection string. A test skips when it is unset rather than
// inventing a database.
const DSNEnv = "KUBBY_TEST_DB_DSN"

// Isolated opens a database in a schema of its own, migrated and empty, and drops it
// when the test ends.
//
// migrationsFrom is the path to the migrations directory relative to the calling
// package, because Go tests run in their own directory.
func Isolated(t *testing.T, migrationsFrom string) *store.DB {
	t.Helper()

	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping tests that need a database", DSNEnv)
	}

	schema := "test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	ctx := context.Background()

	db, err := store.OpenDSN(ctx, dsn+" search_path="+schema, 5)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		db.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		db.Close()
	})

	apply(t, db, schema, migrationsFrom)
	return db
}

// apply runs the migrations into the schema, stripping goose's markers: the files are
// the source of truth for the shape of the database, and a test that built its own copy
// would drift from them.
func apply(t *testing.T, db *store.DB, schema, migrationsFrom string) {
	t.Helper()
	ctx := context.Background()

	files, err := filepath.Glob(filepath.Join(migrationsFrom, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found in %s: %v", migrationsFrom, err)
	}
	sort.Strings(files)

	if _, err := db.Pool().Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if _, err := db.Pool().Exec(ctx, upSection(string(raw))); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(file), err)
		}
	}
}

// upSection is everything between the goose Up marker and the Down one.
func upSection(sql string) string {
	body := sql
	if _, after, found := strings.Cut(body, "-- +goose Up"); found {
		body = after
	}
	if before, _, found := strings.Cut(body, "-- +goose Down"); found {
		body = before
	}

	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-- +goose") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
