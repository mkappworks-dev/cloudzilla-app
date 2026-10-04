package service

import (
	"errors"
	"fmt"
	"html/template"
	"strings"
	"sync"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
)

// ErrEmptyRepo is returned when a repository has no commits.
var ErrEmptyRepo = errors.New("repository is empty")

// ErrRefNotFound is returned when a named ref (branch, tag, or SHA) cannot be resolved.
var ErrRefNotFound = errors.New("ref not found")

// ErrRefMoved means a branch moved between reading its tip and advancing it,
// so the new commit was not applied. Handlers answer 409.
var ErrRefMoved = gitref.ErrMoved

// BranchInfo holds summary information about a git branch.
type BranchInfo struct {
	Name      string
	Hash      string
	IsDefault bool
}

// TagInfo holds summary information about a git tag.
type TagInfo struct {
	Name string
	Hash string
}

// RefsResult holds all branches and tags for a repository.
type RefsResult struct {
	Branches []BranchInfo
	Tags     []TagInfo
}

// BreadcrumbPart represents one segment of a file path breadcrumb navigation.
type BreadcrumbPart struct {
	Name string
	URL  string
}

// CodeLine represents a single numbered line of source code.
type CodeLine struct {
	Num  int
	Text string
	HTML template.HTML `json:"-"`
}

// CodeService provides read and write operations over bare git repositories on disk.
type CodeService struct {
	cfg config.GitConfig
	// treeCache memoizes ListEntriesWithLastCommit results.
	// Key: treeCacheKey; Value: treeCacheEntry. TTL enforced at read time.
	treeCache sync.Map
	// treeCacheKeys is a best-effort insert counter that drives the
	// treeCacheMaxKeys-bounded eviction sweep.
	treeCacheKeys int64
}

// NewCodeService returns a CodeService configured to read repos from cfg.ReposRoot.
func NewCodeService(cfg config.GitConfig) *CodeService {
	return &CodeService{cfg: cfg}
}

func (s *CodeService) repoPath(owner, repoName string) (string, error) {
	return RepoDir(s.cfg.ReposRoot, owner, repoName+".git")
}

// wikiPath returns the filesystem path of the wiki bare repo.
func (s *CodeService) wikiPath(owner, repoName string) (string, error) {
	return RepoDir(s.cfg.ReposRoot, owner, repoName+".wiki.git")
}

func (s *CodeService) openRepo(owner, repoName string) (*gogit.Repository, error) {
	return openRepoAt(s.repoPath(owner, repoName))
}

func (s *CodeService) openWiki(owner, repoName string) (*gogit.Repository, error) {
	return openRepoAt(s.wikiPath(owner, repoName))
}

// An unsafe path names no repository, so callers' not-exists handling covers it.
func openRepoAt(path string, err error) (*gogit.Repository, error) {
	if err != nil {
		return nil, gogit.ErrRepositoryNotExists
	}
	return gogit.PlainOpen(path)
}

// ResolveRef resolves a ref string to a commit and the ref to build URLs
// with: the branch or tag name, the full SHA, or HEAD's branch.
// Priority: branch → tag → SHA → HEAD.
// Returns ErrEmptyRepo if the repo has no commits.
func (s *CodeService) ResolveRef(owner, repoName, ref string) (commit *object.Commit, displayRef string, err error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, "", err
	}
	return resolveRef(repo, ref)
}

func resolveRef(repo *gogit.Repository, ref string) (*object.Commit, string, error) {
	if ref != "" {
		// Try branch
		if branchRef, err := repo.Reference(plumbing.NewBranchReferenceName(ref), true); err == nil {
			if commit, err := repo.CommitObject(branchRef.Hash()); err == nil {
				return commit, ref, nil
			}
		}
		// Try tag
		if tagRef, err := repo.Reference(plumbing.NewTagReferenceName(ref), true); err == nil {
			if commit, err := repo.CommitObject(peelTag(repo, tagRef.Hash())); err == nil {
				return commit, ref, nil
			}
		}
		// Try raw SHA
		hash := plumbing.NewHash(ref)
		if commit, err := repo.CommitObject(hash); err == nil {
			return commit, commit.Hash.String(), nil
		}
		if _, headErr := repo.Head(); headErr != nil {
			if errors.Is(headErr, plumbing.ErrReferenceNotFound) {
				return nil, "", ErrEmptyRepo
			}
			return nil, "", fmt.Errorf("resolve HEAD: %w", headErr)
		}
		return nil, "", fmt.Errorf("%w: %s", ErrRefNotFound, ref)
	}

	// Fall back to HEAD
	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, "", ErrEmptyRepo
		}
		return nil, "", fmt.Errorf("resolve HEAD: %w", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, "", ErrEmptyRepo
		}
		return nil, "", fmt.Errorf("load HEAD commit %s: %w", head.Hash(), err)
	}
	return commit, head.Name().Short(), nil
}

// peelTag follows annotated tags to their target; a non-tag hash comes back unchanged, so a tag of a tree or blob still fails CommitObject.
func peelTag(repo *gogit.Repository, h plumbing.Hash) plumbing.Hash {
	for {
		tag, err := repo.TagObject(h)
		if err != nil {
			return h
		}
		h = tag.Target
	}
}

func buildBreadcrumbs(owner, repoName, ref, path string, isBlob bool) []BreadcrumbPart {
	parts := []BreadcrumbPart{
		{Name: owner, URL: "/" + owner},
		{Name: repoName, URL: "/" + owner + "/" + repoName},
	}
	if path == "" {
		return parts
	}
	segments := strings.Split(path, "/")
	accumulated := ""
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		if accumulated == "" {
			accumulated = seg
		} else {
			accumulated += "/" + seg
		}
		isLast := i == len(segments)-1
		var url string
		if isLast && isBlob {
			url = "/" + owner + "/" + repoName + "/blob/" + ref + "/" + accumulated
		} else {
			url = "/" + owner + "/" + repoName + "/tree/" + ref + "/" + accumulated
		}
		parts = append(parts, BreadcrumbPart{Name: seg, URL: url})
	}
	return parts
}
