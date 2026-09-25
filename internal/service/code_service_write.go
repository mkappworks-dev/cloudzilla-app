package service

import (
	"errors"
	"fmt"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// ErrProfileRepoMissing is returned when the user/org has no bare repo named
// after themselves; callers map this to a redirect to the create-repo flow.
var ErrProfileRepoMissing = errors.New("profile repo does not exist")

// SaveProfileReadme writes README.md to the root of the user's profile repo
// (owner/repoName.git) on defaultBranch. message defaults to "Update profile
// README" when empty. Errors are wrapped with context so handler logs locate
// the failing stage.
func (s *CodeService) SaveProfileReadme(owner, repoName, defaultBranch, content, authorName, authorEmail, message string) error {
	if strings.TrimSpace(defaultBranch) == "" {
		defaultBranch = "main"
	}
	if message == "" {
		message = "Update profile README"
	}

	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		if errors.Is(err, gogit.ErrRepositoryNotExists) {
			return ErrProfileRepoMissing
		}
		return fmt.Errorf("profile readme open: %w", err)
	}

	branchRef := plumbing.NewBranchReferenceName(defaultBranch)
	if err := commitSingleFile(repo, branchRef, authorName, authorEmail, message, "README.md", []byte(content)); err != nil {
		return fmt.Errorf("profile readme commit: %w", err)
	}
	return nil
}
