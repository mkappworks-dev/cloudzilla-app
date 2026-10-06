package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/mkappworks-dev/cloudzilla-app/internal/avatar"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrNotOrgOwner         = errors.New("only org owners can change the organization's picture")
	ErrStorageUnconfigured = errors.New("avatar storage is not configured")
)

// AvatarService stores avatars as objects and points users.avatar_key and
// organizations.avatar_key at them. Keys carry the content hash, so an object
// never changes once written.
type AvatarService struct {
	users   *store.UserStore
	orgs    *store.OrgStore
	isOwner func(ctx context.Context, orgID, userID int64) bool
	backend storage.Backend
}

func NewAvatarService(users *store.UserStore, orgs *store.OrgStore, orgSvc *OrgService) *AvatarService {
	return &AvatarService{users: users, orgs: orgs, isOwner: orgSvc.IsOwner}
}

// WithBackend must be called before the service handles a request.
func (s *AvatarService) WithBackend(b storage.Backend) *AvatarService {
	s.backend = b
	return s
}

func (s *AvatarService) Backend() storage.Backend { return s.backend }

// SetUserAvatar returns the new key.
func (s *AvatarService) SetUserAvatar(ctx context.Context, userID int64, r io.Reader) (string, error) {
	return s.set(ctx, "user", userID, r, s.users.SwapAvatarKey)
}

func (s *AvatarService) RemoveUserAvatar(ctx context.Context, userID int64) error {
	return s.remove(ctx, userID, s.users.SwapAvatarKey)
}

// SetOrgAvatar returns the new key.
func (s *AvatarService) SetOrgAvatar(ctx context.Context, orgID, actorID int64, r io.Reader) (string, error) {
	if !s.isOwner(ctx, orgID, actorID) {
		return "", ErrNotOrgOwner
	}
	return s.set(ctx, "org", orgID, r, s.orgs.SwapAvatarKey)
}

func (s *AvatarService) RemoveOrgAvatar(ctx context.Context, orgID, actorID int64) error {
	if !s.isOwner(ctx, orgID, actorID) {
		return ErrNotOrgOwner
	}
	return s.remove(ctx, orgID, s.orgs.SwapAvatarKey)
}

type swapFunc func(ctx context.Context, id int64, key string) (string, error)

// set writes the object before the row points at it, so a reader never sees a
// key without its object. On a failed swap the new object is removed.
func (s *AvatarService) set(ctx context.Context, kind string, id int64, r io.Reader, swap swapFunc) (string, error) {
	if s.backend == nil {
		return "", ErrStorageUnconfigured
	}
	img, err := avatar.Process(r)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("avatars/%s/%d/%s.%s", kind, id, img.SHA256, img.Ext)
	if err := s.backend.Put(ctx, key, bytes.NewReader(img.Data), int64(len(img.Data))); err != nil {
		return "", err
	}
	old, err := swap(ctx, id, key)
	if err != nil {
		s.DeleteObject(ctx, key)
		return "", err
	}
	if old != key {
		s.DeleteObject(ctx, old)
	}
	return key, nil
}

func (s *AvatarService) remove(ctx context.Context, id int64, swap swapFunc) error {
	old, err := swap(ctx, id, "")
	if err != nil {
		return err
	}
	if s.backend != nil {
		s.DeleteObject(ctx, old)
	}
	return nil
}

// DeleteObject logs a failure instead of returning it: the row no longer
// points at key, so a leftover object is an orphan, not a broken avatar.
func (s *AvatarService) DeleteObject(ctx context.Context, key string) {
	if key == "" || s == nil || s.backend == nil {
		return
	}
	if err := s.backend.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("avatar: delete object failed; leaving an orphan", "key", key, "error", err)
	}
}
