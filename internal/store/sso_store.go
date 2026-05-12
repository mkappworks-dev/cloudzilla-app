package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// SSOStore provides read/write access to the sso_configs table.
// SSOStore provides database operations for SSO provider configuration and assertion replay.
type SSOStore struct{ db *sql.DB }

// NewSSOStore creates a new SSOStore.
// NewSSOStore creates an SSOStore backed by the given database.
func NewSSOStore(db *sql.DB) *SSOStore {
	return &SSOStore{db: db}
}

// GetByProvider returns the SSOConfig for a given provider name ("ldap" or "saml").
// Returns sql.ErrNoRows if no config exists.
func (s *SSOStore) GetByProvider(ctx context.Context, provider string) (*model.SSOConfig, error) {
	var configRaw []byte
	cfg := &model.SSOConfig{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, provider, config, enabled, created_at, updated_at
		 FROM sso_configs WHERE provider = $1`,
		provider,
	).Scan(&cfg.ID, &cfg.Provider, &configRaw, &cfg.Enabled, &cfg.CreatedAt, &cfg.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("sso get by provider: %w", err)
	}
	cfg.Config = make(map[string]string)
	if len(configRaw) > 0 {
		if err := json.Unmarshal(configRaw, &cfg.Config); err != nil {
			return nil, fmt.Errorf("sso config corrupt for provider %s: %w", cfg.Provider, err)
		}
	}
	return cfg, nil
}

// ListAll returns all SSO configs (at most 2 rows — one per provider type).
func (s *SSOStore) ListAll(ctx context.Context) ([]*model.SSOConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, provider, config, enabled, created_at, updated_at FROM sso_configs ORDER BY provider`,
	)
	if err != nil {
		return nil, fmt.Errorf("sso list all: %w", err)
	}
	defer rows.Close()

	var result []*model.SSOConfig
	for rows.Next() {
		var configRaw []byte
		cfg := &model.SSOConfig{}
		if err := rows.Scan(&cfg.ID, &cfg.Provider, &configRaw, &cfg.Enabled, &cfg.CreatedAt, &cfg.UpdatedAt); err != nil {
			return nil, fmt.Errorf("sso list all scan: %w", err)
		}
		cfg.Config = make(map[string]string)
		if len(configRaw) > 0 {
			if err := json.Unmarshal(configRaw, &cfg.Config); err != nil {
				return nil, fmt.Errorf("sso config corrupt for provider %s: %w", cfg.Provider, err)
			}
		}
		result = append(result, cfg)
	}
	return result, rows.Err()
}

// Upsert inserts or updates an SSO config for the given provider.
func (s *SSOStore) Upsert(ctx context.Context, provider string, config map[string]string, enabled bool) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal sso config: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sso_configs (provider, config, enabled, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (provider) DO UPDATE
		   SET config = EXCLUDED.config, enabled = EXCLUDED.enabled, updated_at = NOW()`,
		provider, string(raw), enabled,
	)
	if err != nil {
		return fmt.Errorf("sso upsert: %w", err)
	}
	return nil
}

// GetUserBySSO looks up a user by sso_provider + sso_id.
func (s *SSOStore) GetUserBySSO(ctx context.Context, provider, ssoID string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at
		 FROM users WHERE sso_provider = $1 AND sso_id = $2`,
		provider, ssoID,
	).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sso get user: %w", err)
	}
	return u, nil
}

// IsAssertionUsed reports whether a SAML assertion ID has already been consumed
// and has not yet expired. Returns false (no error) when the assertion is new.
func (s *SSOStore) IsAssertionUsed(ctx context.Context, assertionID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM saml_used_assertions
		 WHERE assertion_id = $1 AND expires_at > NOW()`,
		assertionID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("sso check assertion: %w", err)
	}
	return count > 0, nil
}

// MarkAssertionUsed records a SAML assertion ID as consumed until expiresAt.
// Uses ON CONFLICT DO NOTHING so a concurrent duplicate is silently ignored
// (the first writer wins; both paths return the assertion-already-used error).
func (s *SSOStore) MarkAssertionUsed(ctx context.Context, assertionID string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO saml_used_assertions (assertion_id, expires_at)
		 VALUES ($1, $2)
		 ON CONFLICT (assertion_id) DO NOTHING`,
		assertionID, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("sso mark assertion used: %w", err)
	}
	return nil
}

// ProvisionSSOUser creates a new user account for an SSO login.
func (s *SSOStore) ProvisionSSOUser(ctx context.Context, username, email, ssoProvider, ssoID string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, bio, avatar_url, sso_provider, sso_id, is_invited, created_at, updated_at)
		 VALUES ($1, $2, '', '', '', $3, $4, TRUE, NOW(), NOW())
		 RETURNING id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		           is_superadmin, is_invited, created_at, updated_at`,
		username, email, ssoProvider, ssoID,
	).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sso provision user: %w", err)
	}
	return u, nil
}
