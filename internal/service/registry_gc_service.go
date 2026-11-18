package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	registrySweepInterval = time.Hour
	registrySweepBatch    = 500
)

// RegistryGCService is the hourly sweep that reclaims aborted uploads and unreferenced blobs.
type RegistryGCService struct {
	registry *store.RegistryStore
	cfg      config.RegistryConfig
	backend  storage.Backend
	now      func() time.Time
	// beforeReap runs after a blob is picked and before its transaction, so a test can race a push against the delete.
	beforeReap func(digest string)
}

func NewRegistryGCService(registry *store.RegistryStore, cfg config.RegistryConfig) *RegistryGCService {
	return &RegistryGCService{registry: registry, cfg: cfg, now: time.Now}
}

// WithBackend must be called before the first sweep.
func (s *RegistryGCService) WithBackend(b storage.Backend) *RegistryGCService {
	s.backend = b
	return s
}

func (s *RegistryGCService) WithClock(now func() time.Time) *RegistryGCService {
	s.now = now
	return s
}

func (s *RegistryGCService) WithSweepHook(h func(digest string)) *RegistryGCService {
	s.beforeReap = h
	return s
}

// Run sweeps every hour until ctx ends.
func (s *RegistryGCService) Run(ctx context.Context) {
	ticker := time.NewTicker(registrySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Sweep(ctx); err != nil {
				slog.Error("registry sweep failed", "error", err)
			}
		}
	}
}

// Sweep runs one pass. The phases run in order, and a later phase never acts on what an earlier one did in the same
// pass for the grace, so an aborted push's blob outlives its link by a second grace.
func (s *RegistryGCService) Sweep(ctx context.Context) error {
	now := s.now()
	if err := s.reclaimUploads(ctx, now); err != nil {
		return err
	}
	if err := s.dropUnreferencedLinks(ctx, now); err != nil {
		return err
	}
	if err := s.markUnlinked(ctx, now); err != nil {
		return err
	}
	return s.reapBlobs(ctx, now)
}

func (s *RegistryGCService) reclaimUploads(ctx context.Context, now time.Time) error {
	for {
		paths, n, err := s.registry.DeleteExpiredPendingPackages(ctx, now, registrySweepBatch)
		if err != nil {
			return err
		}
		removeSpoolFiles(paths)
		if n < registrySweepBatch {
			break
		}
	}
	for {
		paths, n, err := s.registry.DeleteExpiredSessions(ctx, now.Add(-s.cfg.UploadTTL), registrySweepBatch)
		if err != nil {
			return err
		}
		removeSpoolFiles(paths)
		if n < registrySweepBatch {
			return nil
		}
	}
}

func removeSpoolFiles(paths []string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Warn("registry sweep: remove spool file failed", "path", p, "error", err)
		}
	}
}

func (s *RegistryGCService) dropUnreferencedLinks(ctx context.Context, now time.Time) error {
	for {
		n, err := s.registry.DeleteUnreferencedLinks(ctx, now.Add(-s.cfg.GCGrace), registrySweepBatch)
		if err != nil || n < registrySweepBatch {
			return err
		}
	}
}

func (s *RegistryGCService) markUnlinked(ctx context.Context, now time.Time) error {
	for {
		n, err := s.registry.MarkUnlinkedBlobs(ctx, now, registrySweepBatch)
		if err != nil || n < registrySweepBatch {
			return err
		}
	}
}

func (s *RegistryGCService) reapBlobs(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-s.cfg.GCGrace)
	after := ""
	for {
		batch, err := s.registry.UnlinkedBlobsAfter(ctx, cutoff, after, registrySweepBatch)
		if err != nil {
			return err
		}
		for _, digest := range batch {
			if s.beforeReap != nil {
				s.beforeReap(digest)
			}
			if _, err := s.registry.ReapBlob(ctx, digest, cutoff, func(ctx context.Context) error {
				return s.deleteObject(ctx, digest)
			}); err != nil {
				slog.Warn("registry sweep: reap blob failed; retrying next run", "digest", digest, "error", err)
			}
		}
		if len(batch) < registrySweepBatch {
			return nil
		}
		after = batch[len(batch)-1]
	}
}

func (s *RegistryGCService) deleteObject(ctx context.Context, digest string) error {
	if s.backend == nil {
		return ErrStorageUnconfigured
	}
	if err := s.backend.Delete(context.WithoutCancel(ctx), blobKey(digest)); err != nil {
		return fmt.Errorf("delete blob object: %w", err)
	}
	return nil
}
