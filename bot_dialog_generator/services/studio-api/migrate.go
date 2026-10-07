package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.up.sql
var migrationFiles embed.FS

// migrationLockID serializes concurrent migration runs (an arbitrary constant).
const migrationLockID = 7_310_552_840

// Migrate applies the pending migrations in order, each in its own transaction, and
// records them in schema_migrations. It must run as the schema owner, never as the
// application role, which cannot create tables.
func Migrate(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("migrate: lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck // the lock also ends with the session

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	for _, name := range names {
		version, _, _ := strings.Cut(strings.TrimPrefix(name, "migrations/"), "_")
		var applied bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&applied); err != nil {
			return fmt.Errorf("migrate: check %s: %w", version, err)
		}
		if applied {
			continue
		}
		sql, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			// The simple protocol runs a file with many statements in one call.
			if _, err := tx.Conn().PgConn().Exec(ctx, string(sql)).ReadAll(); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
			return err
		})
		if err != nil {
			return fmt.Errorf("migrate: apply %s: %w", name, err)
		}
	}
	return nil
}
