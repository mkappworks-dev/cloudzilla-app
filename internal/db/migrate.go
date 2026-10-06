package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(db *sql.DB) error {
	return runMigrations(db, migrationsFS)
}

func runMigrations(db *sql.DB, migFS embed.FS) error {
	// Create migrations table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Get already-applied migrations
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	var applied []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan migration version: %w", err)
		}
		applied = append(applied, version)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migrations: %w", err)
	}
	appliedSet := make(map[string]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}

	files, err := migrationFiles(migFS)
	if err != nil {
		return err
	}

	for _, f := range files {
		version := strings.TrimSuffix(f, ".sql")
		if appliedSet[version] {
			continue
		}

		content, err := migFS.ReadFile("migrations/" + f)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", f, err)
		}

		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return fmt.Errorf("record migration %s: %w", f, err)
		}

		slog.Info("applied migration", "file", f)
	}

	return nil
}

// Pending lists the embedded migrations not yet recorded in schema_migrations,
// oldest first. Without that table every migration is pending.
func Pending(ctx context.Context, db *sql.DB) ([]string, error) {
	files, err := migrationFiles(migrationsFS)
	if err != nil {
		return nil, err
	}

	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("look up migrations table: %w", err)
	}
	applied := map[string]bool{}
	if exists {
		rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
		if err != nil {
			return nil, fmt.Errorf("query applied migrations: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var version string
			if err := rows.Scan(&version); err != nil {
				return nil, fmt.Errorf("scan migration version: %w", err)
			}
			applied[version] = true
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate migrations: %w", err)
		}
	}

	var pending []string
	for _, f := range files {
		if version := strings.TrimSuffix(f, ".sql"); !applied[version] {
			pending = append(pending, version)
		}
	}
	return pending, nil
}

func migrationFiles(migFS fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(migFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}
