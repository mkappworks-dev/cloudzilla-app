package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// SSOService handles LDAP and SAML authentication flows.
// SSOService manages LDAP and SAML SSO authentication flows.
type SSOService struct {
	store       *store.SSOStore
	users       *store.UserStore
	cfg         config.AuthConfig
	siteSetting *SiteSettingService
}

// NewSSOService creates a new SSOService.
// NewSSOService creates an SSOService with the given stores, auth config, and site settings.
func NewSSOService(s *store.SSOStore, users *store.UserStore, cfg config.AuthConfig, siteSetting *SiteSettingService) *SSOService {
	return &SSOService{store: s, users: users, cfg: cfg, siteSetting: siteSetting}
}

// GetConfig returns the SSOConfig for the given provider.
// Returns nil (no error) if no config has been saved yet.
func (s *SSOService) GetConfig(ctx context.Context, provider string) (*model.SSOConfig, error) {
	cfg, err := s.store.GetByProvider(ctx, provider)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return cfg, err
}

// ListConfigs returns all stored SSO configs.
func (s *SSOService) ListConfigs(ctx context.Context) ([]*model.SSOConfig, error) {
	return s.store.ListAll(ctx)
}

// ErrSSOIncomplete refuses to leave a provider on without a setting its sign-in
// needs: turning it on before they're saved, or clearing one while it's on.
var ErrSSOIncomplete = errors.New("sso provider lacks a setting sign-in needs")

// SetConfig replaces a provider's settings and state outright, unchecked; the
// admin pages go through SaveSettings and SetEnabled, which keep ErrSSOIncomplete's rule.
func (s *SSOService) SetConfig(ctx context.Context, provider string, config map[string]string, enabled bool) error {
	if provider != "ldap" && provider != "saml" {
		return fmt.Errorf("unknown sso provider: %s", provider)
	}
	return s.store.Upsert(ctx, provider, config, enabled)
}

// SaveSettings replaces a provider's settings and keeps it on or off as it was.
func (s *SSOService) SaveSettings(ctx context.Context, provider string, config map[string]string) error {
	if provider != "ldap" && provider != "saml" {
		return fmt.Errorf("unknown sso provider: %s", provider)
	}
	if !model.SSOSettingsReady(provider, config) {
		cur, err := s.GetConfig(ctx, provider)
		if err != nil {
			return err
		}
		if cur != nil && cur.Enabled {
			return ErrSSOIncomplete
		}
	}
	return s.store.SaveConfig(ctx, provider, config)
}

// SetEnabled turns a provider on or off and leaves its settings alone.
func (s *SSOService) SetEnabled(ctx context.Context, provider string, enabled bool) error {
	if provider != "ldap" && provider != "saml" {
		return fmt.Errorf("unknown sso provider: %s", provider)
	}
	cfg, err := s.GetConfig(ctx, provider)
	if err != nil {
		return err
	}
	if enabled && !cfg.Ready() {
		return ErrSSOIncomplete
	}
	if cfg == nil {
		return nil
	}
	return s.store.SetEnabled(ctx, provider, enabled)
}

// ProviderEnabled reports whether provider ("ldap" or "saml") is configured and on.
func (s *SSOService) ProviderEnabled(ctx context.Context, provider string) bool {
	cfg, err := s.store.GetByProvider(ctx, provider)
	return err == nil && cfg.Enabled
}
