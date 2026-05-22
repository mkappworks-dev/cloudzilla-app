package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrReleaseTagInUse is returned by Create when a release already exists for
// the requested tag in the repository.
var ErrReleaseTagInUse = errors.New("a release already exists for this tag")

// ErrInvalidTagName is returned when the tag string isn't safe to land in git
// storage as a ref. We refuse these up-front rather than relying on go-git's
// looser plumbing checks.
var ErrInvalidTagName = errors.New(`tag name must match ^[A-Za-z0-9._/-]{1,255}$ and contain no ".."`)

// tagNamePattern excludes '@' to keep clear of reflog syntax.
var tagNamePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)

// validTagName reports whether tagName is safe to use as a git tag ref. Beyond
// the character allowlist it rejects "..", which in a tag like "../etc/passwd"
// would escape refs/tags/ when written to git storage.
func validTagName(tagName string) bool {
	return tagNamePattern.MatchString(tagName) && !strings.Contains(tagName, "..")
}

type ReleaseService struct {
	releases *store.ReleaseStore
	repos    *store.RepoStore
	code     *CodeService
}

func NewReleaseService(releases *store.ReleaseStore, repos *store.RepoStore, code *CodeService) *ReleaseService {
	return &ReleaseService{releases: releases, repos: repos, code: code}
}

// Create creates the tag on the tip of target (default branch when unset) when
// the tag does not yet exist.
func (s *ReleaseService) Create(ctx context.Context, owner, repoName, tagName, target, name, body string, isPrerelease, isDraft bool, authorID int64) (*model.Release, error) {
	if !validTagName(tagName) {
		return nil, ErrInvalidTagName
	}

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
		// Race-loser path: another in-flight Create on the same tag won the
		// unique-violation; surface the same friendly sentinel as the pre-check
		// so the handler emits one toast wording.
		if errors.Is(err, store.ErrReleaseTagInUseStore) {
			return nil, ErrReleaseTagInUse
		}
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
	if !validTagName(tagName) {
		return nil, ErrInvalidTagName
	}
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

// ErrReleaseAlreadyPublished is returned by Publish on a non-draft. Publishing
// is one-way; delete + recreate is the only way back.
var ErrReleaseAlreadyPublished = errors.New("release is already published")

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
