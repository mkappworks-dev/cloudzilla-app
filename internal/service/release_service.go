package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ReleaseService manages repository release creation, updates, and deletion.
type ReleaseService struct {
	releases *store.ReleaseStore
	repos    *store.RepoStore
	code     *CodeService
}

// NewReleaseService creates a ReleaseService backed by the given stores and code service.
func NewReleaseService(releases *store.ReleaseStore, repos *store.RepoStore, code *CodeService) *ReleaseService {
	return &ReleaseService{releases: releases, repos: repos, code: code}
}

func (s *ReleaseService) Create(ctx context.Context, owner, repoName, tagName, name, body string, isPrerelease, isDraft bool, authorID int64) (*model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}

	// Validate tag exists
	refs, err := s.code.ListRefs(owner, repoName, repo.DefaultBranch)
	if err != nil {
		return nil, fmt.Errorf("could not list refs: %w", err)
	}
	tagFound := false
	for _, t := range refs.Tags {
		if t.Name == tagName {
			tagFound = true
			break
		}
	}
	if !tagFound {
		return nil, fmt.Errorf("tag %q does not exist in repository", tagName)
	}

	var publishedAt *time.Time
	if !isDraft {
		now := time.Now()
		publishedAt = &now
	}

	r := &model.Release{
		RepoID:       repo.ID,
		TagName:      tagName,
		Name:         name,
		Body:         body,
		IsPrerelease: isPrerelease,
		IsDraft:      isDraft,
		AuthorID:     authorID,
		PublishedAt:  publishedAt,
	}
	if err := s.releases.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReleaseService) CountPublished(ctx context.Context, repoID int64) (int, error) {
	return s.releases.CountPublished(ctx, repoID)
}

func (s *ReleaseService) ListByRepo(ctx context.Context, owner, repoName string) ([]model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.releases.ListByRepo(ctx, repo.ID)
}

// Falls back to created_at when PublishedAt is nil.
func (s *ReleaseService) RecentForRepo(ctx context.Context, owner, repoName string, limit int) ([]model.Release, error) {
	all, err := s.ListByRepo(ctx, owner, repoName)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		return releaseSortTime(all[i]).After(releaseSortTime(all[j]))
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func releaseSortTime(r model.Release) time.Time {
	if r.PublishedAt != nil {
		return *r.PublishedAt
	}
	return r.CreatedAt
}

func (s *ReleaseService) GetByTag(ctx context.Context, owner, repoName, tagName string) (*model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.releases.GetByTag(ctx, repo.ID, tagName)
}

func (s *ReleaseService) GetByID(ctx context.Context, owner, repoName string, id int64) (*model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.releases.GetByID(ctx, id, repo.ID)
}

func (s *ReleaseService) GetLatest(ctx context.Context, owner, repoName string) (*model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.releases.GetLatest(ctx, repo.ID)
}

func (s *ReleaseService) Update(ctx context.Context, owner, repoName string, id int64, tagName, name, body string, isPrerelease, isDraft bool) (*model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	r, err := s.releases.GetByID(ctx, id, repo.ID)
	if err != nil {
		return nil, fmt.Errorf("release not found: %w", err)
	}

	r.TagName = tagName
	r.Name = name
	r.Body = body
	r.IsPrerelease = isPrerelease
	r.IsDraft = isDraft
	if !isDraft && r.PublishedAt == nil {
		now := time.Now()
		r.PublishedAt = &now
	}

	if err := s.releases.Update(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReleaseService) Delete(ctx context.Context, owner, repoName string, id int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.releases.Delete(ctx, id, repo.ID)
}
