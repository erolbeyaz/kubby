package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the pgx driver for database/sql
	"github.com/pressly/goose/v3"

	"github.com/erolbeyaz/kubby/migrations"
)

// Migrate brings the database up to the schema this binary was built against.
//
// Run by Kubby itself at start rather than by an operator beforehand. A deployment is one
// container: there is no host to run a migration tool from, the image has no shell to run
// one in, and a separate step before the first start is a step somebody misses exactly
// once — after which the server starts, answers its health probes, and fails on the first
// real request with a message about a missing table.
//
// Also what makes an upgrade work: a new image carries its own new migrations and applies
// them when it starts, so upgrading is pulling a tag.
func Migrate(ctx context.Context, dsn string, logger *slog.Logger) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{logger: logger})

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set the migration dialect: %w", err)
	}

	// A separate connection from the pool, closed as soon as this is done: migrations run
	// once at start and holding a pooled connection for them serves nothing.
	db, err := sql.Open("pgx/v5", dsn)
	if err != nil {
		return fmt.Errorf("open the database for migration: %w", err)
	}
	defer func() { _ = db.Close() }()

	migrateCtx, cancel := context.WithTimeout(ctx, migrationTimeout)
	defer cancel()

	before, _ := goose.GetDBVersionContext(migrateCtx, db)

	// goose takes an advisory lock, so several replicas starting together apply the
	// migrations once rather than racing each other through them.
	if err := goose.UpContext(migrateCtx, db, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	after, err := goose.GetDBVersionContext(migrateCtx, db)
	if err != nil {
		return fmt.Errorf("read the schema version: %w", err)
	}

	if before == after {
		logger.Info("schema is up to date", slog.Int64("version", after))
	} else {
		logger.Info("schema migrated",
			slog.Int64("from", before), slog.Int64("to", after))
	}
	return nil
}

// SchemaVersion reports the applied version, for the readiness check.
func (db *DB) SchemaVersion(ctx context.Context) (int64, error) {
	var version int64
	err := db.pool.QueryRow(ctx,
		`SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1`).
		Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read the schema version: %w", err)
	}
	return version, nil
}

// gooseLogger sends goose's output through the structured logger rather than to stdout,
// so a migration at start looks like every other line in the log.
type gooseLogger struct{ logger *slog.Logger }

func (g gooseLogger) Printf(format string, v ...any) {
	g.logger.Info("migration", slog.String("detail", trimNewline(fmt.Sprintf(format, v...))))
}

func (g gooseLogger) Fatalf(format string, v ...any) {
	// Not fatal here: the caller decides what a failed migration means, and a library
	// calling os.Exit takes the decision away from it.
	g.logger.Error("migration failed", slog.String("detail", trimNewline(fmt.Sprintf(format, v...))))
}

func trimNewline(text string) string {
	for len(text) > 0 && (text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	return text
}

// Generous: a migration on a large table is slow, and timing one out halfway is worse
// than waiting.
const migrationTimeout = 10 * time.Minute
