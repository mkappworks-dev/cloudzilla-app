package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrPackageQuota matches every *PackageQuotaError.
var ErrPackageQuota = errors.New("package quota reached")

// PackageQuotaError names usage and quota; the registry answers it as DENIED with this message.
type PackageQuotaError struct {
	Used  int64
	Limit int64
}

func (e *PackageQuotaError) Error() string {
	return fmt.Sprintf("package storage quota reached (%s of %s)", FormatBytes(e.Used), FormatBytes(e.Limit))
}

func (e *PackageQuotaError) Is(target error) bool { return target == ErrPackageQuota }

// PackageService owns packages: generic files, listing, linking, deletion and the quota rule.
type PackageService struct {
	packages *store.PackageStore
	users    *store.UserStore
	quota    config.QuotaConfig
}

func NewPackageService(packages *store.PackageStore, users *store.UserStore, quota config.QuotaConfig) *PackageService {
	return &PackageService{packages: packages, users: users, quota: quota}
}

// Usage is what the path owner o pays for.
func (s *PackageService) Usage(ctx context.Context, o QuotaOwner) (int64, error) {
	return s.packages.OwnerUsage(ctx, o.UserID, o.OrgID)
}

// limit is 0, meaning off, for a superadmin's personal account, as for the git storage quota.
func (s *PackageService) limit(ctx context.Context, o QuotaOwner) (int64, error) {
	if o.OrgID != 0 {
		return s.quota.Org.PackageBytes, nil
	}
	if s.quota.User.PackageBytes <= 0 {
		return 0, nil
	}
	u, err := s.users.GetByID(ctx, o.UserID)
	if err != nil {
		return 0, fmt.Errorf("load quota owner: %w", err)
	}
	if u.IsSuperadmin {
		return 0, nil
	}
	return s.quota.User.PackageBytes, nil
}

// CheckQuota refuses adding bytes to o when that would pass its package quota. It checks and the caller commits
// afterwards, so uploads racing each other can overshoot by as many as run at once.
func (s *PackageService) CheckQuota(ctx context.Context, o QuotaOwner, adding int64) error {
	limit, err := s.limit(ctx, o)
	if err != nil || limit <= 0 {
		return err
	}
	used, err := s.Usage(ctx, o)
	if err != nil {
		return err
	}
	if used+adding > limit {
		return &PackageQuotaError{Used: used, Limit: limit}
	}
	return nil
}
