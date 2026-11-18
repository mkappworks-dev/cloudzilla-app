package service

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// RegistryGCService is the hourly sweep that reclaims aborted uploads and unreferenced blobs.
type RegistryGCService struct {
	registry *store.RegistryStore
	cfg      config.RegistryConfig
}

func NewRegistryGCService(registry *store.RegistryStore, cfg config.RegistryConfig) *RegistryGCService {
	return &RegistryGCService{registry: registry, cfg: cfg}
}
