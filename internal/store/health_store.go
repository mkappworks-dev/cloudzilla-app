package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
)

// HealthStore answers readiness probes about the database.
type HealthStore struct {
	db *sql.DB
}

// NewHealthStore creates a HealthStore backed by the given database.
func NewHealthStore(db *sql.DB) *HealthStore {
	return &HealthStore{db: db}
}

func (s *HealthStore) Ping(ctx context.Context) error {
	if s.db == nil {
		return errors.New("no database configured")
	}
	return s.db.PingContext(ctx)
}

func (s *HealthStore) PendingMigrations(ctx context.Context) ([]string, error) {
	if s.db == nil {
		return nil, errors.New("no database configured")
	}
	return db.Pending(ctx, s.db)
}
