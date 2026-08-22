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
		for _, stmt := range upStatements(string(raw)) {
			if _, err := db.Pool().Exec(ctx, stmt); err != nil {
				t.Fatalf("apply %s: %v", filepath.Base(file), err)
			}
		}
	}
}

func upStatements(content string) []string {
	up, _, found := strings.Cut(content, "-- +goose Down")
	if !found {
		up = content
	}
	_, up, found = strings.Cut(up, "-- +goose Up")
	if !found {
		return nil
	}

	var out []string
	for _, block := range strings.Split(up, "-- +goose StatementBegin") {
		body, _, ok := strings.Cut(block, "-- +goose StatementEnd")
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(body); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
