package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// OAuthAppStore provides database operations for registered OAuth applications.
type OAuthAppStore struct{ db *sql.DB }

// NewOAuthAppStore creates an OAuthAppStore backed by the given database.
func NewOAuthAppStore(db *sql.DB) *OAuthAppStore { return &OAuthAppStore{db: db} }

func (s *OAuthAppStore) Create(ctx context.Context, app *model.OAuthApp) error {
	app.RedirectURIsRaw = strings.Join(app.RedirectURIs, ",")
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO oauth_apps (owner_id, name, client_id, client_secret, redirect_uris, homepage_url, description)
         VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		app.OwnerID, app.Name, app.ClientID, app.ClientSecret,
		app.RedirectURIsRaw, app.HomepageURL, app.Description,
	).Scan(&app.ID, &app.CreatedAt)
	if err != nil {
		return fmt.Errorf("oauth_app create: %w", err)
	}
	return nil
}

func (s *OAuthAppStore) GetByClientID(ctx context.Context, clientID string) (*model.OAuthApp, error) {
	app := &model.OAuthApp{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, name, client_id, client_secret, redirect_uris, homepage_url, description, created_at
         FROM oauth_apps WHERE client_id = $1`,
		clientID,
	).Scan(&app.ID, &app.OwnerID, &app.Name, &app.ClientID, &app.ClientSecret,
		&app.RedirectURIsRaw, &app.HomepageURL, &app.Description, &app.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("oauth_app get by client_id: %w", err)
	}
	if app.RedirectURIsRaw != "" {
		app.RedirectURIs = strings.Split(app.RedirectURIsRaw, ",")
	}
	return app, nil
}

func (s *OAuthAppStore) ListByOwner(ctx context.Context, ownerID int64) ([]model.OAuthApp, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, name, client_id, client_secret, redirect_uris, homepage_url, description, created_at
         FROM oauth_apps WHERE owner_id = $1 ORDER BY created_at DESC`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("oauth_app list by owner: %w", err)
	}
	defer rows.Close()
	var apps []model.OAuthApp
	for rows.Next() {
		var a model.OAuthApp
		if err := rows.Scan(&a.ID, &a.OwnerID, &a.Name, &a.ClientID, &a.ClientSecret,
			&a.RedirectURIsRaw, &a.HomepageURL, &a.Description, &a.CreatedAt); err != nil {
			return nil, err
		}
		if a.RedirectURIsRaw != "" {
			a.RedirectURIs = strings.Split(a.RedirectURIsRaw, ",")
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func (s *OAuthAppStore) Delete(ctx context.Context, id, ownerID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM oauth_apps WHERE id = $1 AND owner_id = $2`,
		id, ownerID,
	)
	return err
}
