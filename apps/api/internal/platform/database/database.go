// Package database owns the PostgreSQL connection pool, schema migrations and
// small helpers for interpreting driver errors.
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Gestusition/field-day/apps/api/migrations"
)

// Connect opens a connection pool and verifies it with a ping.
func Connect(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database: parse url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}

// Migrator returns a golang-migrate instance backed by the embedded migrations.
// The caller must Close it.
func Migrator(pool *pgxpool.Pool) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: source: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
	drv, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		return nil, fmt.Errorf("migrate: init: %w", err)
	}
	return m, nil
}

// MigrateUp applies every pending migration.
func MigrateUp(pool *pgxpool.Pool, log *slog.Logger) error {
	m, err := Migrator(pool)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	v, dirty, _ := m.Version()
	log.Info("database schema is current", "version", v, "dirty", dirty)
	return nil
}

// InTx runs fn inside a transaction, committing on success.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, fn)
}

// IsNotFound reports whether err means "no row".
func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUniqueViolation reports a unique constraint failure, optionally limited to
// the named constraints or indexes.
func IsUniqueViolation(err error, constraints ...string) bool {
	return isCode(err, "23505", constraints)
}

// IsForeignKeyViolation reports a foreign key failure, optionally limited to
// the named constraints.
func IsForeignKeyViolation(err error, constraints ...string) bool {
	return isCode(err, "23503", constraints)
}

// IsCheckViolation reports a CHECK constraint failure.
func IsCheckViolation(err error, constraints ...string) bool {
	return isCode(err, "23514", constraints)
}

func isCode(err error, code string, constraints []string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		return false
	}
	if len(constraints) == 0 {
		return true
	}
	for _, c := range constraints {
		if pgErr.ConstraintName == c {
			return true
		}
	}
	return false
}
