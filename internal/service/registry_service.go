package service

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// RegistryService is the OCI registry's logic: token issue and verify, upload sessions, blobs, manifests, tags.
type RegistryService struct {
	registry *store.RegistryStore
	packages *PackageService
	cfg      config.RegistryConfig
}

func NewRegistryService(registry *store.RegistryStore, packages *PackageService, cfg config.RegistryConfig) *RegistryService {
	return &RegistryService{registry: registry, packages: packages, cfg: cfg}
}
