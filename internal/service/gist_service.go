package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	maxGistFiles    = 10
	maxGistFileSize = 1 * 1024 * 1024 // 1 MB
)

var ErrGistNotFound = errors.New("gist not found")

// GistService manages gist creation, updates, and access control.
type GistService struct {
	gists *store.GistStore
}

// NewGistService creates a GistService backed by the given gist store.
func NewGistService(gists *store.GistStore) *GistService {
	return &GistService{gists: gists}
}

func generateGistID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validateGistFiles(files []model.GistFile) error {
	if len(files) == 0 {
		return errors.New("at least one file is required")
	}
	if len(files) > maxGistFiles {
		return fmt.Errorf("gist cannot have more than %d files", maxGistFiles)
	}
	seen := make(map[string]struct{}, len(files))
	for _, f := range files {
		if strings.TrimSpace(f.Filename) == "" {
			return errors.New("filename must not be empty")
		}
		if _, dup := seen[f.Filename]; dup {
			return fmt.Errorf("duplicate filename: %s", f.Filename)
		}
		seen[f.Filename] = struct{}{}
		if len(f.Content) > maxGistFileSize {
			return fmt.Errorf("file %q exceeds 1 MB limit", f.Filename)
		}
	}
	return nil
}

func (s *GistService) Create(ctx context.Context, ownerID int64, ownerName, description string, public bool, files []model.GistFile) (*model.Gist, error) {
	if err := validateGistFiles(files); err != nil {
		return nil, err
	}
	id, err := generateGistID()
	if err != nil {
		return nil, fmt.Errorf("gist id generation: %w", err)
	}
	g := &model.Gist{
		ID:          id,
		OwnerID:     ownerID,
		OwnerName:   ownerName,
		Description: description,
		Public:      public,
	}
	if err := s.gists.Create(ctx, g, files); err != nil {
		return nil, fmt.Errorf("gist create: %w", err)
	}
	return g, nil
}

func (s *GistService) Get(ctx context.Context, id string) (*model.Gist, []model.GistFile, error) {
	return s.gists.Get(ctx, id)
}

func (s *GistService) ListByOwner(ctx context.Context, ownerID int64, page, pageSize int) ([]model.Gist, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.gists.ListByOwner(ctx, ownerID, page, pageSize)
}

func (s *GistService) Explore(ctx context.Context, page, pageSize int) ([]model.Gist, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.gists.ListPublic(ctx, page, pageSize)
}

func (s *GistService) ListPublicByOwner(ctx context.Context, ownerID int64, page, pageSize int) ([]model.Gist, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.gists.ListPublicByOwner(ctx, ownerID, page, pageSize)
}

func (s *GistService) Update(ctx context.Context, gistID string, requesterID int64, description string, public bool, files []model.GistFile) error {
	g, _, err := s.gists.Get(ctx, gistID)
	if err != nil {
		return ErrGistNotFound
	}
	if g.OwnerID != requesterID {
		return ErrForbidden
	}
	if err := validateGistFiles(files); err != nil {
		return err
	}
	g.Description = description
	g.Public = public
	return s.gists.Update(ctx, g, files)
}

func (s *GistService) ListWithCounts(ctx context.Context, ownerFilter string) ([]model.GistListRow, error) {
	return s.gists.ListWithCounts(ctx, ownerFilter)
}

func (s *GistService) ListPrivateByOwner(ctx context.Context, ownerID int64, page, pageSize int) ([]model.Gist, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.gists.ListPrivateByOwner(ctx, ownerID, page, pageSize)
}

func (s *GistService) CountByUser(ctx context.Context, userID int64) (int, error) {
	return s.gists.CountByOwner(ctx, userID)
}

func (s *GistService) Delete(ctx context.Context, gistID string, requesterID int64) error {
	g, _, err := s.gists.Get(ctx, gistID)
	if err != nil {
		return ErrGistNotFound
	}
	if g.OwnerID != requesterID {
		return ErrForbidden
	}
	return s.gists.Delete(ctx, gistID)
}
