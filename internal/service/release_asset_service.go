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
	"path"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrReleaseAssetNotFound  = errors.New("release asset not found")
	ErrReleaseAssetTooLarge  = errors.New("release asset is too large")
	ErrReleaseAssetLimit     = errors.New("release has too many assets")
	ErrReleaseAssetNameTaken = errors.New("release already has an asset with this name")
	ErrReleaseAssetName      = errors.New("release asset name is empty")
)

const maxReleaseAssetNameLen = 100

// ReleaseAssetService stores files attached to releases. It does not decide
// who may upload or read: callers check the repo first.
type ReleaseAssetService struct {
	assets  *store.ReleaseAssetStore
	backend storage.Backend
	cfg     config.ReleaseConfig
}

func NewReleaseAssetService(assets *store.ReleaseAssetStore, cfg config.ReleaseConfig) *ReleaseAssetService {
	return &ReleaseAssetService{assets: assets, cfg: cfg}
}

// WithBackend must be called before the service handles a request.
func (s *ReleaseAssetService) WithBackend(b storage.Backend) *ReleaseAssetService {
	s.backend = b
	return s
}

func (s *ReleaseAssetService) MaxBytes() int64 { return s.cfg.MaxAssetBytes }

// CleanReleaseAssetName keeps the last path element and replaces anything
// outside [A-Za-z0-9._-] with "-", so the name is safe in a URL, a header and a
// file system. Over-long names keep their extension.
func CleanReleaseAssetName(filename string) (string, error) {
	base := path.Base(strings.ReplaceAll(filename, `\`, "/"))
	var b strings.Builder
	for _, c := range base {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteByte('-')
		}
	}
	name := b.String()
	if len(name) > maxReleaseAssetNameLen {
		ext := path.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:maxReleaseAssetNameLen-len(ext)] + ext
	}
	if strings.Trim(name, ".-_") == "" {
		return "", ErrReleaseAssetName
	}
	return name, nil
}

// Upload spools r to a temp file while hashing it, because S3 needs the length
// before the first byte, then writes the object before the row so a row never
// names a missing object.
func (s *ReleaseAssetService) Upload(ctx context.Context, repoID, releaseID, uploaderID int64, filename string, r io.Reader) (*model.ReleaseAsset, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	name, err := CleanReleaseAssetName(filename)
	if err != nil {
		return nil, err
	}
	if err := s.precheck(ctx, releaseID, name); err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "cz-release-asset-*")
	if err != nil {
		return nil, fmt.Errorf("release asset spool: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(r, s.cfg.MaxAssetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("release asset spool: %w", err)
	}
	if n > s.cfg.MaxAssetBytes {
		return nil, ErrReleaseAssetTooLarge
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("release asset spool: %w", err)
	}

	a := &model.ReleaseAsset{
		ReleaseID:  releaseID,
		RepoID:     repoID,
		Name:       name,
		Size:       n,
		SHA256:     hex.EncodeToString(sum.Sum(nil)),
		StorageKey: fmt.Sprintf("releases/repo/%d/%d/%s.bin", repoID, releaseID, attachment.NewToken()),
		UploaderID: &uploaderID,
	}
	if err := s.backend.Put(ctx, a.StorageKey, tmp, n); err != nil {
		return nil, fmt.Errorf("store release asset: %w", err)
	}
	if err := s.assets.Create(ctx, a, s.cfg.MaxAssets); err != nil {
		s.deleteObject(ctx, a.StorageKey)
		switch {
		case errors.Is(err, store.ErrReleaseAssetNameTaken):
			return nil, ErrReleaseAssetNameTaken
		case errors.Is(err, store.ErrReleaseAssetLimit):
			return nil, ErrReleaseAssetLimit
		}
		return nil, err
	}
	return a, nil
}

// precheck refuses before the body is read; the store repeats both checks
// under a lock for concurrent uploads.
func (s *ReleaseAssetService) precheck(ctx context.Context, releaseID int64, name string) error {
	if taken, err := s.assets.NameExists(ctx, releaseID, name); err != nil {
		return err
	} else if taken {
		return ErrReleaseAssetNameTaken
	}
	if n, err := s.assets.Count(ctx, releaseID); err != nil {
		return err
	} else if n >= s.cfg.MaxAssets {
		return ErrReleaseAssetLimit
	}
	return nil
}

func (s *ReleaseAssetService) Get(ctx context.Context, releaseID, id int64) (*model.ReleaseAsset, error) {
	a, err := s.assets.Get(ctx, releaseID, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReleaseAssetNotFound
	}
	return a, err
}

func (s *ReleaseAssetService) GetByName(ctx context.Context, releaseID int64, name string) (*model.ReleaseAsset, error) {
	a, err := s.assets.GetByName(ctx, releaseID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReleaseAssetNotFound
	}
	return a, err
}

func (s *ReleaseAssetService) ListByRelease(ctx context.Context, releaseID int64) ([]model.ReleaseAsset, error) {
	return s.assets.ListByRelease(ctx, releaseID)
}

// ListByReleases groups the assets of several releases by release id.
func (s *ReleaseAssetService) ListByReleases(ctx context.Context, releaseIDs []int64) (map[int64][]model.ReleaseAsset, error) {
	list, err := s.assets.ListByReleases(ctx, releaseIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]model.ReleaseAsset, len(releaseIDs))
	for _, a := range list {
		out[a.ReleaseID] = append(out[a.ReleaseID], a)
	}
	return out, nil
}

// Open returns storage.ErrNotFound when the row outlived its object.
func (s *ReleaseAssetService) Open(ctx context.Context, a *model.ReleaseAsset) (io.ReadCloser, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	return s.backend.Get(ctx, a.StorageKey)
}

// Delete removes the row first, then the object: a row that outlived its
// object would serve a 404 for a file the page still lists.
func (s *ReleaseAssetService) Delete(ctx context.Context, a *model.ReleaseAsset) error {
	if err := s.assets.Delete(ctx, a.ID); err != nil {
		return err
	}
	s.DeleteObjects(ctx, []model.ReleaseAsset{*a})
	return nil
}

// DeleteObjects runs after the database change commits. A failed delete is
// logged and the object stays as an orphan, which is safe to remove by hand.
func (s *ReleaseAssetService) DeleteObjects(ctx context.Context, list []model.ReleaseAsset) {
	if s.backend == nil {
		return
	}
	for _, a := range list {
		s.deleteObject(ctx, a.StorageKey)
	}
}

// listByReposDeletedBefore must run before the purge: it cascades to the rows.
func (s *ReleaseAssetService) listByReposDeletedBefore(ctx context.Context, cutoff time.Time) ([]model.ReleaseAsset, error) {
	return s.assets.ListByReposDeletedBefore(ctx, cutoff)
}

func (s *ReleaseAssetService) deleteObject(ctx context.Context, key string) {
	if err := s.backend.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("release asset: delete object failed; leaving an orphan", "key", key, "error", err)
	}
}
