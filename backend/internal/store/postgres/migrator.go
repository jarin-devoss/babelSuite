package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migration/*.sql
var migrationFS embed.FS

// migrate applies all pending SQL migrations in version order.
//
// A schema_migrations table tracks which versions have been applied. Each
// migration runs in its own transaction so a failure leaves the schema in
// the last-known-good state. A PostgreSQL advisory lock prevents concurrent
// migration runs when multiple replicas start simultaneously.
func (s *Store) migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire connection: %w", err)
	}
	defer conn.Release()

	// Advisory lock — key 5441414244 spells "BABBD" in ASCII.
	// Blocks until the lock is available so concurrent replicas serialise here.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(5441414244)`); err != nil {
		return fmt.Errorf("migrate: advisory lock: %w", err)
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock(5441414244)`) //nolint:errcheck

	if _, err := conn.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return err
	}

	files, err := sortedMigrationFiles()
	if err != nil {
		return err
	}

	for _, f := range files {
		if applied[f.version] {
			continue
		}

		sql, err := fs.ReadFile(migrationFS, f.path)
		if err != nil {
			return fmt.Errorf("migrate: read %s: %w", f.path, err)
		}

		if err := applyMigration(ctx, conn, f.version, string(sql)); err != nil {
			return fmt.Errorf("migrate: apply %s: %w", f.version, err)
		}
	}

	return nil
}

type migrationFile struct {
	version string
	path    string
}

func sortedMigrationFiles() ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrationFS, "migration")
	if err != nil {
		return nil, fmt.Errorf("migrate: read dir: %w", err)
	}

	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// Version is the filename prefix before the first underscore or the
		// full stem when no underscore is present (e.g. "0001" from "0001_initial.sql").
		stem := strings.TrimSuffix(e.Name(), ".sql")
		version := stem
		if idx := strings.IndexByte(stem, '_'); idx >= 0 {
			version = stem[:idx]
		}
		files = append(files, migrationFile{
			version: version,
			path:    "migration/" + e.Name(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})
	return files, nil
}

func appliedVersions(ctx context.Context, conn interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (map[string]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: query applied versions: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func applyMigration(ctx context.Context, conn interface {
	Begin(context.Context) (pgx.Tx, error)
}, version, sql string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
