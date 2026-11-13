package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrOrgNameUnchanged = errors.New("the organization already has that name")
	ErrOrgChanged       = store.ErrOrgChanged
)

// RenameOrg gives org, as seen by a requester who owns it, the name newName.
// The owner directory under the repos root and the rows move together; the old
// name stays reachable as an alias. Errors: ErrForbidden, ErrInvalidOrgName and
// ErrOrgNameUnchanged (422), ErrOrgNameTaken, ErrRepoSyncing and ErrOrgChanged
// (409).
func (s *OrgService) RenameOrg(ctx context.Context, org *model.Organization, requesterID int64, newName string) error {
	if !s.IsOwner(ctx, org.ID, requesterID) {
		return fmt.Errorf("only org owners can rename an organization: %w", ErrForbidden)
	}
	if err := ValidateOwnerName(newName); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidOrgName, err)
	}
	if newName == org.Name {
		return ErrOrgNameUnchanged
	}
	if !strings.EqualFold(newName, org.Name) {
		if taken, err := s.users.OwnerNameTaken(ctx, newName); err != nil {
			return err
		} else if taken {
			return ErrOrgNameTaken
		}
	}
	repos, err := s.repos.GetByOrgID(ctx, org.ID)
	if err != nil {
		return err
	}
	for _, r := range repos {
		if s.repo == nil {
			break
		}
		if err := s.repo.checkNotSyncing(ctx, r.ID); err != nil {
			return err
		}
	}

	// One rename moves every repo, wiki and trashed copy at once, and a failure
	// leaves either all of them or none at the new path.
	oldDir := filepath.Join(s.cfg.ReposRoot, org.Name)
	newDir := filepath.Join(s.cfg.ReposRoot, newName)
	if pathTakenByOther(newDir, oldDir) {
		return ErrOrgNameTaken
	}
	moved, err := renameDirs([]dirMove{{from: oldDir, to: newDir}})
	if err != nil {
		return fmt.Errorf("move org dir: %w", err)
	}
	if err := s.orgs.Rename(ctx, org.ID, org.Name, newName); err != nil {
		revertDirs(moved)
		if errors.Is(err, store.ErrOrgNameTaken) {
			return ErrOrgNameTaken
		}
		return err
	}
	return nil
}

// ResolveAlias returns the org that used to be called name, or sql.ErrNoRows.
// An org profile is public, so there is no visibility to check.
func (s *OrgService) ResolveAlias(ctx context.Context, name string) (*model.Organization, error) {
	return s.orgs.ResolveAlias(ctx, name)
}
