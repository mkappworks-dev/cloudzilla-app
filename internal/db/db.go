package db

import (
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// driverName maps config driver names to registered driver names
func driverName(cfgDriver string) string {
	switch cfgDriver {
	case "sqlite3":
		return "sqlite"
	case "postgres", "postgresql":
		return "pgx"
	default:
		return cfgDriver
	}
}

func Connect(cfg config.DatabaseConfig) (*sql.DB, error) {
	driver := driverName(cfg.Driver)
	dsn := cfg.DSN

	database, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	database.SetMaxOpenConns(cfg.MaxOpenConns)
	database.SetMaxIdleConns(cfg.MaxIdleConns)

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	// SQLite pragmas
	if driver == "sqlite" {
		pragmas := []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA foreign_keys=ON",
			"PRAGMA busy_timeout=5000",
		}
		for _, p := range pragmas {
			if _, err := database.Exec(p); err != nil {
				return nil, fmt.Errorf("pragma %q: %w", p, err)
			}
		}
	}

	return database, nil
}
