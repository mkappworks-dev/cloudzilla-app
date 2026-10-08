package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var ErrAttachmentNotFound = errors.New("attachment not found")

const (
	// AttachmentGrace is how long an upload may sit unreferenced: long enough
	// to write a comment around it before the sweep can take it.
	AttachmentGrace = 24 * time.Hour

	attachmentSweepInterval = time.Hour
	attachmentSweepBatch    = 100
)

// AttachmentService stores images pasted into a repo's markdown. It does not
// decide who may upload or read: callers check the repo first.
type AttachmentService struct {
	attachments *store.AttachmentStore
	backend     storage.Backend
}

func NewAttachmentService(attachments *store.AttachmentStore) *AttachmentService {
	return &AttachmentService{attachments: attachments}
}

// WithBackend must be called before the service handles a request.
func (s *AttachmentService) WithBackend(b storage.Backend) *AttachmentService {
	s.backend = b
	return s
}

// Upload writes the object before the row, so a row never names a missing
// object. If the row fails, the object is removed.
func (s *AttachmentService) Upload(ctx context.Context, repoID, uploaderID int64, r io.Reader) (*model.Attachment, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	file, err := attachment.Validate(r)
	if err != nil {
		return nil, err
	}
	token := attachment.NewToken()
	a := &model.Attachment{
		Token:       token,
		RepoID:      repoID,
		UploaderID:  &uploaderID,
		StorageKey:  fmt.Sprintf("attachments/repo/%d/%s.%s", repoID, token, file.Ext),
		ContentType: file.ContentType,
		Size:        int64(len(file.Data)),
	}
	if err := s.backend.Put(ctx, a.StorageKey, bytes.NewReader(file.Data), a.Size); err != nil {
		return nil, fmt.Errorf("store attachment: %w", err)
	}
	if err := s.attachments.Create(ctx, a); err != nil {
		s.deleteObject(ctx, a.StorageKey)
		return nil, err
	}
	return a, nil
}

func (s *AttachmentService) Get(ctx context.Context, token string) (*model.Attachment, error) {
	a, err := s.attachments.Get(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAttachmentNotFound
	}
	return a, err
}

// Open returns storage.ErrNotFound when the row outlived its object.
func (s *AttachmentService) Open(ctx context.Context, a *model.Attachment) (io.ReadCloser, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	return s.backend.Get(ctx, a.StorageKey)
}

// DeleteForRepo removes a repo's attachments for good. A failure to delete one
// object leaves its row for the sweep to retry.
func (s *AttachmentService) DeleteForRepo(ctx context.Context, repoID int64) error {
	list, err := s.attachments.ListByRepo(ctx, repoID)
	if err != nil {
		return err
	}
	for _, a := range list {
		if err := s.remove(ctx, a); err != nil {
			slog.Warn("attachment: delete failed; the sweep will retry", "token", a.Token, "error", err)
		}
	}
	return nil
}

// Sweep deletes attachments of repos that no longer exist, and uploads older
// than grace that no stored markdown references: ones never used, or whose
// comment was edited or deleted. It returns how many it deleted.
func (s *AttachmentService) Sweep(ctx context.Context, grace time.Duration) (int, error) {
	deleted := 0
	for {
		batch, err := s.attachments.ListSweepable(ctx, time.Now().Add(-grace), attachmentSweepBatch)
		if err != nil {
			return deleted, err
		}
		progress := 0
		for _, a := range batch {
			if err := s.remove(ctx, a); err != nil {
				slog.Warn("attachment: sweep delete failed", "token", a.Token, "error", err)
				continue
			}
			progress++
		}
		deleted += progress
		// A batch that made no progress would come back unchanged.
		if len(batch) < attachmentSweepBatch || progress == 0 {
			return deleted, nil
		}
	}
}

// Run sweeps every hour until ctx ends.
func (s *AttachmentService) Run(ctx context.Context) {
	ticker := time.NewTicker(attachmentSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := s.Sweep(ctx, AttachmentGrace); err != nil {
				slog.Error("attachment sweep failed", "error", err)
			} else if n > 0 {
				slog.Info("attachment sweep completed", "deleted", n)
			}
		}
	}
}

// remove deletes the object first: if the row delete then fails, the sweep
// finds the row again, whereas a row deleted first would strand its object.
func (s *AttachmentService) remove(ctx context.Context, a model.Attachment) error {
	if s.backend != nil {
		if err := s.backend.Delete(context.WithoutCancel(ctx), a.StorageKey); err != nil {
			return err
		}
	}
	return s.attachments.Delete(ctx, a.Token)
}

func (s *AttachmentService) deleteObject(ctx context.Context, key string) {
	if err := s.backend.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("attachment: delete object failed; leaving an orphan", "key", key, "error", err)
	}
}
