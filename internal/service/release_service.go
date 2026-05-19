package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrReleaseTagInUse is returned by Create when a release already exists for
// the requested tag in the repository.
var ErrReleaseTagInUse = errors.New("a release already exists for this tag")

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

// Create records a release for tagName. When the tag does not yet exist it is
// created on the latest commit of target (falling back to the default branch).
func (s *ReleaseService) Create(ctx context.Context, owner, repoName, tagName, target, name, body string, isPrerelease, isDraft bool, authorID int64) (*model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}

	if _, err := s.releases.GetByTag(ctx, repo.ID, tagName); err == nil {
		return nil, ErrReleaseTagInUse
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("could not check existing releases: %w", err)
	}

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
		if target == "" {
			target = repo.DefaultBranch
		}
		if err := s.code.CreateTag(owner, repoName, tagName, target); err != nil {
			return nil, fmt.Errorf("could not create tag %q on %q: %w", tagName, target, err)
		}
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

// EditName updates only the human-readable name of a release.
func (s *ReleaseService) EditName(ctx context.Context, owner, repoName string, id int64, name string) (*model.Release, error) {
	r, err := s.loadByID(ctx, owner, repoName, id)
	if err != nil {
		return nil, err
	}
	r.Name = name
	if err := s.releases.Update(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// ErrReleaseAlreadyPublished is returned by Publish when the release is no
// longer a draft. Publishing is a one-way transition; the only way back is
// delete + recreate.
var ErrReleaseAlreadyPublished = errors.New("release is already published")

// Publish promotes a draft release to a published one. It clears is_draft and
// stamps published_at if not already set. Returns ErrReleaseAlreadyPublished
// when called on a release that is not currently a draft.
func (s *ReleaseService) Publish(ctx context.Context, owner, repoName string, id int64) (*model.Release, error) {
	r, err := s.loadByID(ctx, owner, repoName, id)
	if err != nil {
		return nil, err
	}
	if !r.IsDraft {
		return nil, ErrReleaseAlreadyPublished
	}
	r.IsDraft = false
	if r.PublishedAt == nil {
		now := time.Now()
		r.PublishedAt = &now
	}
	if err := s.releases.Update(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// EditPrerelease toggles the pre-release flag without touching tag, name, body,
// or draft state.
func (s *ReleaseService) EditPrerelease(ctx context.Context, owner, repoName string, id int64, isPrerelease bool) (*model.Release, error) {
	r, err := s.loadByID(ctx, owner, repoName, id)
	if err != nil {
		return nil, err
	}
	r.IsPrerelease = isPrerelease
	if err := s.releases.Update(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// EditBody updates only the markdown body of a release.
func (s *ReleaseService) EditBody(ctx context.Context, owner, repoName string, id int64, body string) (*model.Release, error) {
	r, err := s.loadByID(ctx, owner, repoName, id)
	if err != nil {
		return nil, err
	}
	r.Body = body
	if err := s.releases.Update(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *ReleaseService) loadByID(ctx context.Context, owner, repoName string, id int64) (*model.Release, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	r, err := s.releases.GetByID(ctx, id, repo.ID)
	if err != nil {
		return nil, fmt.Errorf("release not found: %w", err)
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
