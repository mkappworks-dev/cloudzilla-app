package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ImportTarget is the namespace an import lands in: OwnerID for a personal
// repo, OrgID for an org repo.
type ImportTarget struct {
	ActorID   int64
	OwnerName string
	OwnerID   int64
	OrgID     int64
}

var errImportOwner = fmt.Errorf("you can import only into your account or an organization you own: %w", ErrForbidden)

// ResolveImportTarget maps ownerName to the actor's own account (also for "")
// or to an org the actor owns.
func (s *RepoService) ResolveImportTarget(ctx context.Context, actorID int64, actorUsername, ownerName string) (ImportTarget, error) {
	if ownerName == "" || ownerName == actorUsername {
		if _, err := s.personalOwner(ctx, actorID, actorUsername); err != nil {
			return ImportTarget{}, err
		}
		return ImportTarget{ActorID: actorID, OwnerName: actorUsername, OwnerID: actorID}, nil
	}
	org, err := s.orgs.GetByName(ctx, ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportTarget{}, errImportOwner
	}
	if err != nil {
		return ImportTarget{}, err
	}
	if !s.isOrgOwner(ctx, org.ID, actorID) {
		return ImportTarget{}, errImportOwner
	}
	return ImportTarget{ActorID: actorID, OwnerName: org.Name, OrgID: org.ID}, nil
}

// CheckImportName fails fast before a long clone; claimRepo checks again at publish.
func (s *RepoService) CheckImportName(ctx context.Context, ownerName, name string) error {
	if err := ValidateRepoName(name); err != nil {
		return fmt.Errorf("invalid repository name: %w", err)
	}
	held, err := s.repos.NameHeld(ctx, ownerName, name)
	if err != nil {
		return err
	}
	if held {
		return ErrRepoNameTaken
	}
	return nil
}

// CreateFromImport adopts srcDir, a bare repo an import cloned, as t's repo
// name. srcDir is left in place on failure; the caller removes it.
func (s *RepoService) CreateFromImport(ctx context.Context, t ImportTarget, name, description string, private bool, defaultBranch, srcDir string) (*model.Repository, error) {
	if err := ValidateRepoName(name); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}
	if t.OrgID != 0 {
		if !s.isOrgOwner(ctx, t.OrgID, t.ActorID) {
			return nil, fmt.Errorf("no longer an owner of %s: %w", t.OwnerName, ErrForbidden)
		}
	} else if _, err := s.personalOwner(ctx, t.OwnerID, t.OwnerName); err != nil {
		return nil, err
	}

	gitDir, err := claimRepo(ctx, s.repos, s.cfg.ReposRoot, t.OwnerName, name)
	if err != nil {
		return nil, err
	}
	r := &model.Repository{
		OwnerID:       t.OwnerID,
		OrgID:         t.OrgID,
		CreatedBy:     t.ActorID,
		OwnerName:     t.OwnerName,
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: defaultBranch,
	}
	if err := s.repos.CreateWithOwnerName(ctx, r); err != nil {
		abandonNewRepo(ctx, s.repos, 0, gitDir)
		return nil, repoNameErr("create imported repo", err)
	}
	// os.Rename won't replace a directory, so the empty claim goes first. The
	// row already holds the name, so a concurrent create still sees it taken.
	if err := os.Remove(gitDir); err != nil {
		abandonNewRepo(ctx, s.repos, r.ID, gitDir)
		return nil, fmt.Errorf("remove claimed dir: %w", err)
	}
	if err := os.Rename(srcDir, gitDir); err != nil {
		abandonNewRepo(ctx, s.repos, r.ID, gitDir)
		return nil, fmt.Errorf("move imported repo into place: %w", err)
	}
	return r, nil
}
