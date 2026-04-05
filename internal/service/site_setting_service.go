package service

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
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
	count, err := s.userStore.CountAll(ctx)
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

func (s *SiteSettingService) GetAll(ctx context.Context) ([]model.SiteSetting, error) {
	return s.store.GetAll(ctx)
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
