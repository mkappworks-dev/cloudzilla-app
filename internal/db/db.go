package db

import (
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func Connect(cfg config.DatabaseConfig) (*sql.DB, error) {
	driver := "pgx"
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

	return database, nil
}
