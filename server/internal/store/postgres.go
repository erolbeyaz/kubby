// Package store owns all PostgreSQL access.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/erolbeyaz/kubby/internal/config"
)

// DB wraps the connection pool. It is safe for concurrent use.
type DB struct {
	pool *pgxpool.Pool
}

// Open creates the pool and verifies connectivity once. A failure here is fatal at
// startup; later outages are surfaced through Ping and the readiness probe.
func Open(ctx context.Context, cfg config.DBConfig) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		// cfg.DSN carries the password, so report the redacted form instead.
		return nil, fmt.Errorf("parse database config (%s): %w", cfg.Redacted(), err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool (%s): %w", cfg.Redacted(), err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database (%s): %w", cfg.Redacted(), err)
	}
	return &DB{pool: pool}, nil
}

// Pool exposes the underlying pool for repositories.
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Ping reports whether the database is currently reachable. Used by /readyz.
func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.pool == nil {
		return fmt.Errorf("database pool is not initialised")
	}
	return db.pool.Ping(ctx)
}

// Close releases every pooled connection.
func (db *DB) Close() {
	if db != nil && db.pool != nil {
		db.pool.Close()
	}
}

// OpenDSN opens a pool from a raw DSN. Used by integration tests, which are given a
// connection string directly rather than a full config.
func OpenDSN(ctx context.Context, dsn string, maxConns int32) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database DSN: %w", err)
	}
	poolCfg.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return &DB{pool: pool}, nil
}
