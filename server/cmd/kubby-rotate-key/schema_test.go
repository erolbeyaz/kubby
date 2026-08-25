package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/erolbeyaz/kubby/internal/store"
)

// applySchema runs the project's migrations into an isolated schema so rotation tests
// never touch real data.
func applySchema(t *testing.T, db *store.DB, schema string) {
	t.Helper()
	ctx := context.Background()

	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
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
		statements, skipped := upStatements(string(raw))
		// A migration whose SQL sits outside a StatementBegin/End block used to be
		// skipped in silence, and the rotation test then ran against a schema that was
		// missing columns the real database has. Saying so is the whole point: a test
		// that quietly tests less than it claims is worse than one that fails.
		if skipped != "" {
			t.Fatalf("%s has SQL outside a goose statement block, which this helper cannot apply:\n%s",
				filepath.Base(file), skipped)
		}
		for _, stmt := range statements {
			if _, err := db.Pool().Exec(ctx, stmt); err != nil {
				t.Fatalf("apply %s: %v", filepath.Base(file), err)
			}
		}
	}
}

// upStatements returns the Up blocks, and separately whatever SQL was left outside one so
// the caller can refuse to run against a schema it did not fully build.
func upStatements(content string) (statements []string, skipped string) {
	up, _, found := strings.Cut(content, "-- +goose Down")
	if !found {
		up = content
	}
	_, up, found = strings.Cut(up, "-- +goose Up")
	if !found {
		return nil, ""
	}

	var loose []string
	for _, block := range strings.Split(up, "-- +goose StatementBegin") {
		body, rest, ok := strings.Cut(block, "-- +goose StatementEnd")
		if !ok {
			// Before the first StatementBegin, or after the last StatementEnd.
			if sql := meaningfulSQL(block); sql != "" {
				loose = append(loose, sql)
			}
			continue
		}
		if trimmed := strings.TrimSpace(body); trimmed != "" {
			statements = append(statements, trimmed)
		}
		if sql := meaningfulSQL(rest); sql != "" {
			loose = append(loose, sql)
		}
	}
	return statements, strings.Join(loose, "\n")
}

// meaningfulSQL ignores blank lines and comments, which are the only things that
// legitimately sit between blocks.
func meaningfulSQL(block string) string {
	var kept []string
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "\n")
}
