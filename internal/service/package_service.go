package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
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
	limits   config.PackagesConfig
	backend  storage.Backend
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

// WithLimits sets the generic file size cap.
func (s *PackageService) WithLimits(c config.PackagesConfig) *PackageService {
	s.limits = c
	return s
}

// WithBackend must be called before the service handles a generic file.
func (s *PackageService) WithBackend(b storage.Backend) *PackageService {
	s.backend = b
	return s
}

func (s *PackageService) MaxFileBytes() int64 { return s.limits.MaxFileBytes }

var (
	ErrPackageName       = errors.New("invalid package name, version or filename")
	ErrPackageNameTaken  = errors.New("package name is linked to another repository")
	ErrPackageFileExists = errors.New("package file already exists")
	ErrPackageFileTooBig = errors.New("package file is too large")
	ErrPackageNotFound   = errors.New("package not found")
)

var genericSegment = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

// ValidGenericSegment reports whether s may be a generic package name, version or filename. It never cleans: a name
// that needs cleaning is refused, so what is stored is what the caller typed.
func ValidGenericSegment(s string) bool {
	return genericSegment.MatchString(s) && s != "." && s != ".."
}

// UploadGenericFile spools body to a temp file while hashing it, because S3 needs the length before the first byte.
// The size cap and the quota are enforced as bytes arrive, the object is put before the row, and a refused upload
// leaves neither. The caller has checked that uploaderID may write repo.
func (s *PackageService) UploadGenericFile(ctx context.Context, repo *model.Repository, uploaderID int64, name, version, filename string, body io.Reader) (*model.PackageFile, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	if !ValidGenericSegment(name) || !ValidGenericSegment(version) || !ValidGenericSegment(filename) {
		return nil, ErrPackageName
	}
	owner := RepoQuotaOwner(repo)
	if linked, err := s.packages.GenericRepoID(ctx, owner.UserID, owner.OrgID, name); err == nil && linked != repo.ID {
		return nil, ErrPackageNameTaken
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, err := s.packages.GenericFileExists(ctx, repo.ID, name, version, filename); err != nil {
		return nil, err
	} else if exists {
		return nil, ErrPackageFileExists
	}

	allowed := s.limits.MaxFileBytes
	var quotaErr *PackageQuotaError
	if limit, err := s.limit(ctx, owner); err != nil {
		return nil, err
	} else if limit > 0 {
		used, err := s.Usage(ctx, owner)
		if err != nil {
			return nil, err
		}
		if room := max(limit-used, 0); room < allowed {
			allowed, quotaErr = room, &PackageQuotaError{Used: used, Limit: limit}
		}
	}

	tmp, err := os.CreateTemp("", "cz-package-file-*")
	if err != nil {
		return nil, fmt.Errorf("package file spool: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(body, allowed+1))
	if err != nil {
		return nil, fmt.Errorf("package file spool: %w", err)
	}
	if n > allowed {
		if quotaErr != nil {
			return nil, quotaErr
		}
		return nil, ErrPackageFileTooBig
	}
	// Another upload may have used the room since the check above.
	if err := s.CheckQuota(ctx, owner, n); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("package file spool: %w", err)
	}

	f := &model.PackageFile{
		Filename:   filename,
		Size:       n,
		SHA256:     hex.EncodeToString(sum.Sum(nil)),
		StorageKey: fmt.Sprintf("packages/generic/repo/%d/%s.bin", repo.ID, attachment.NewToken()),
		UploaderID: &uploaderID,
	}
	if err := s.backend.Put(ctx, f.StorageKey, tmp, n); err != nil {
		return nil, fmt.Errorf("store package file: %w", err)
	}
	err = s.packages.AddGenericFile(ctx, owner.UserID, owner.OrgID, repo.ID, uploaderID, name, version, f)
	if err != nil {
		s.deleteObject(ctx, f.StorageKey)
		switch {
		case errors.Is(err, store.ErrPackageNameTaken):
			return nil, ErrPackageNameTaken
		case errors.Is(err, store.ErrPackageFileExists):
			return nil, ErrPackageFileExists
		}
		return nil, err
	}
	return f, nil
}

// GenericFile returns ErrPackageNotFound for a missing file.
func (s *PackageService) GenericFile(ctx context.Context, repoID int64, name, version, filename string) (*model.PackageFile, error) {
	f, err := s.packages.GenericFile(ctx, repoID, name, version, filename)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPackageNotFound
	}
	return f, err
}

// OpenFile returns storage.ErrNotFound when the row outlived its object.
func (s *PackageService) OpenFile(ctx context.Context, f *model.PackageFile) (io.ReadCloser, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	return s.backend.Get(ctx, f.StorageKey)
}

func (s *PackageService) ListGeneric(ctx context.Context, repoID int64) ([]model.GenericPackageSummary, error) {
	return s.packages.ListGeneric(ctx, repoID)
}

// GenericVersions returns ErrPackageNotFound for a missing package.
func (s *PackageService) GenericVersions(ctx context.Context, repoID int64, name string) (*model.Package, []model.GenericVersionFiles, error) {
	p, v, err := s.packages.GenericVersions(ctx, repoID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrPackageNotFound
	}
	return p, v, err
}

// DeleteGenericFile removes the row first, then the object, so a row never names a missing object.
func (s *PackageService) DeleteGenericFile(ctx context.Context, repoID int64, name, version, filename string) error {
	key, err := s.packages.DeleteGenericFile(ctx, repoID, name, version, filename)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPackageNotFound
	}
	if err != nil {
		return err
	}
	s.deleteObject(ctx, key)
	return nil
}

func (s *PackageService) DeleteGenericVersion(ctx context.Context, repoID int64, name, version string) error {
	keys, err := s.packages.DeleteGenericVersion(ctx, repoID, name, version)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPackageNotFound
	}
	if err != nil {
		return err
	}
	for _, k := range keys {
		s.deleteObject(ctx, k)
	}
	return nil
}

func (s *PackageService) deleteObject(ctx context.Context, key string) {
	if s.backend == nil {
		return
	}
	if err := s.backend.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("package file: delete object failed; leaving an orphan", "key", key, "error", err)
	}
}
