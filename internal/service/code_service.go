package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks/cloudzilla/internal/config"
)

// ErrEmptyRepo is returned when a repository has no commits.
var ErrEmptyRepo = errors.New("repository is empty")

// ErrRefNotFound is returned when a named ref (branch, tag, or SHA) cannot be resolved.
var ErrRefNotFound = errors.New("ref not found")

type BranchInfo struct {
	Name      string
	Hash      string
	IsDefault bool
}

type TagInfo struct {
	Name string
	Hash string
}

type RefsResult struct {
	Branches []BranchInfo
	Tags     []TagInfo
}

type BreadcrumbPart struct {
	Name string
	URL  string
}

type CodeLine struct {
	Num  int
	Text string
}

type CodeService struct {
	cfg config.GitConfig
}

func NewCodeService(cfg config.GitConfig) *CodeService {
	return &CodeService{cfg: cfg}
}

func (s *CodeService) repoPath(owner, repoName string) string {
	return filepath.Join(s.cfg.ReposRoot, owner, repoName+".git")
}

// wikiPath returns the filesystem path of the wiki bare repo.
func (s *CodeService) wikiPath(owner, repoName string) string {
	return filepath.Join(s.cfg.ReposRoot, owner, repoName+".wiki.git")
}

// ResolveRef resolves a ref string to a commit. Priority: branch → tag → SHA → HEAD.
// Returns ErrEmptyRepo if the repo has no commits.
func (s *CodeService) ResolveRef(owner, repoName, ref string) (*object.Commit, string, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
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
			if commit, err := repo.CommitObject(tagRef.Hash()); err == nil {
				return commit, ref, nil
			}
		}
		// Try raw SHA
		hash := plumbing.NewHash(ref)
		if commit, err := repo.CommitObject(hash); err == nil {
			return commit, ref[:7], nil
		}
		return nil, "", fmt.Errorf("%w: %s", ErrRefNotFound, ref)
	}

	// Fall back to HEAD
	head, err := repo.Head()
	if err != nil {
		return nil, "", ErrEmptyRepo
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, "", ErrEmptyRepo
	}
	displayRef := head.Name().Short()
	return commit, displayRef, nil
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
