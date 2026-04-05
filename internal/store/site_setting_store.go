package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// SiteSettingStore provides database operations for instance-wide site settings.
type SiteSettingStore struct {
	db *sql.DB
}

// NewSiteSettingStore creates a SiteSettingStore backed by the given database.
func NewSiteSettingStore(db *sql.DB) *SiteSettingStore {
	return &SiteSettingStore{db: db}
}

func (s *SiteSettingStore) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM site_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("site_setting get %q: %w", key, err)
	}
	return value, nil
}

func (s *SiteSettingStore) GetAll(ctx context.Context) ([]model.SiteSetting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM site_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("site_setting get all: %w", err)
	}
	defer rows.Close()
	var settings []model.SiteSetting
	for rows.Next() {
		var s model.SiteSetting
		if err := rows.Scan(&s.Key, &s.Value); err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}
	return settings, rows.Err()
}

func (s *SiteSettingStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO site_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("site_setting set %q: %w", key, err)
	}
	return nil
}
