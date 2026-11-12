package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// SiteSettingService manages instance-wide settings such as registration and login toggles.
type SiteSettingService struct {
	store     *store.SiteSettingStore
	userStore *store.UserStore
	setupDone atomic.Bool
	mu        sync.RWMutex
	cache     map[string]string
}

// NewSiteSettingService creates a SiteSettingService backed by the given stores.
func NewSiteSettingService(s *store.SiteSettingStore, u *store.UserStore) *SiteSettingService {
	return &SiteSettingService{
		store:     s,
		userStore: u,
		cache:     make(map[string]string),
	}
}

func (s *SiteSettingService) IsSetupComplete(ctx context.Context) bool {
	if s.setupDone.Load() {
		return true
	}
	count, err := s.userStore.CountAccounts(ctx)
	if err != nil || count == 0 {
		return false
	}
	s.setupDone.Store(true)
	return true
}

func (s *SiteSettingService) loadCache(ctx context.Context) {
	settings, err := s.store.GetAll(ctx)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, setting := range settings {
		s.cache[setting.Key] = setting.Value
	}
}

func (s *SiteSettingService) getBool(ctx context.Context, key string, defaultVal bool) bool {
	s.mu.RLock()
	v, ok := s.cache[key]
	s.mu.RUnlock()
	if !ok {
		s.loadCache(ctx)
		s.mu.RLock()
		v, ok = s.cache[key]
		s.mu.RUnlock()
	}
	if !ok {
		return defaultVal
	}
	return v == "true"
}

func (s *SiteSettingService) AllowLogin(ctx context.Context) bool {
	return s.getBool(ctx, "allow_login", true)
}

func (s *SiteSettingService) AllowRegistration(ctx context.Context) bool {
	return s.getBool(ctx, "allow_registration", true)
}

// termsKey holds Markdown, not a toggle, so GetAll hides it from the feature-flag list.
const termsKey = "terms_content"

// MaxTermsLength bounds the stored terms text so the admin form can't bloat site_settings.
const MaxTermsLength = 50_000

var ErrTermsTooLong = errors.New("terms text is too long")

const defaultTerms = `# Terms of Service

These terms apply to everyone who uses this Cloudzilla instance. The instance administrator can change them at any time.

## Your account

You are responsible for activity under your account and for keeping your credentials secret. Tell an administrator if you
think someone else has access to it.

## Your content

You keep the rights to what you push, post and upload. By storing it here you allow this instance to host, display and
transfer it as the features you use require. Only publish content you have the right to share.

## Organizations

If you create an organization you are responsible for its content and conduct, and for who you let in.

## Acceptable use

Do not use this instance to break the law, to attack or harass others, to distribute malware, or to disrupt the service.
An administrator may remove content or suspend accounts that do.

## No warranty

This instance is provided as is. The administrator is not liable for lost data, so keep your own copies.

## Contact

Questions about these terms go to an instance administrator.
`

func (s *SiteSettingService) GetAll(ctx context.Context) ([]model.SiteSetting, error) {
	all, err := s.store.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, st := range all {
		if st.Key != termsKey {
			out = append(out, st)
		}
	}
	return out, nil
}

// Terms returns the administrator's terms, or the default text while none is saved.
func (s *SiteSettingService) Terms(ctx context.Context) string {
	s.mu.RLock()
	v, ok := s.cache[termsKey]
	s.mu.RUnlock()
	if !ok {
		s.loadCache(ctx)
		s.mu.RLock()
		v, ok = s.cache[termsKey]
		s.mu.RUnlock()
	}
	if !ok || strings.TrimSpace(v) == "" {
		return defaultTerms
	}
	return v
}

// SetTerms saves the terms; an empty value restores the default text.
func (s *SiteSettingService) SetTerms(ctx context.Context, markdown string) error {
	markdown = strings.TrimSpace(markdown)
	if len(markdown) > MaxTermsLength {
		return ErrTermsTooLong
	}
	return s.Set(ctx, termsKey, markdown)
}

func (s *SiteSettingService) Set(ctx context.Context, key, value string) error {
	if err := s.store.Set(ctx, key, value); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
	return nil
}
