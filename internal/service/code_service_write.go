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

func (s *CodeService) SaveProfileReadme(owner, repoName, defaultBranch, content string, author GitAuthor, message string) (RefUpdate, error) {
	if strings.TrimSpace(defaultBranch) == "" {
		defaultBranch = "main"
	}
	if message == "" {
		message = "Update profile README"
	}

	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		if errors.Is(err, gogit.ErrRepositoryNotExists) {
			return RefUpdate{}, ErrProfileRepoMissing
		}
		return RefUpdate{}, fmt.Errorf("profile readme open: %w", err)
	}

	branchRef := plumbing.NewBranchReferenceName(defaultBranch)
	upd, err := commitSingleFile(repo, branchRef, author, message, "README.md", []byte(content))
	if err != nil {
		return RefUpdate{}, fmt.Errorf("profile readme commit: %w", err)
	}
	return upd, nil
}
