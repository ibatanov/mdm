package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

func Run(ctx context.Context, db *sql.DB) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return err
	}

	entries, err := files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := isMigrationApplied(ctx, db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		raw, err := files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		query := strings.TrimSpace(string(raw))
		if query == "" {
			if err := markMigrationApplied(ctx, db, name); err != nil {
				return err
			}
			continue
		}

		if err := applyMigration(ctx, db, name, query); err != nil {
			return err
		}
	}

	return nil
}

func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	const query = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}
	return nil
}

func isMigrationApplied(ctx context.Context, db *sql.DB, name string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM schema_migrations
			WHERE name = $1
		)
	`
	var exists bool
	if err := db.QueryRowContext(ctx, query, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s applied: %w", name, err)
	}
	return exists, nil
}

func applyMigration(ctx context.Context, db *sql.DB, name, query string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s transaction: %w", name, err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if err := markMigrationAppliedTx(ctx, tx, name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	tx = nil
	return nil
}

func markMigrationApplied(ctx context.Context, db *sql.DB, name string) error {
	const query = `
		INSERT INTO schema_migrations (name)
		VALUES ($1)
		ON CONFLICT (name) DO NOTHING
	`
	if _, err := db.ExecContext(ctx, query, name); err != nil {
		return fmt.Errorf("mark empty migration %s applied: %w", name, err)
	}
	return nil
}

func markMigrationAppliedTx(ctx context.Context, tx *sql.Tx, name string) error {
	const query = `
		INSERT INTO schema_migrations (name)
		VALUES ($1)
		ON CONFLICT (name) DO NOTHING
	`
	if _, err := tx.ExecContext(ctx, query, name); err != nil {
		return fmt.Errorf("mark migration %s applied: %w", name, err)
	}
	return nil
}
