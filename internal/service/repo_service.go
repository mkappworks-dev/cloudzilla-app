package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type RepoService struct {
	repos *store.RepoStore
	users *store.UserStore
	orgs  *store.OrgStore
	cfg   config.GitConfig
}

func NewRepoService(repos *store.RepoStore, users *store.UserStore, orgs *store.OrgStore, cfg config.GitConfig) *RepoService {
	return &RepoService{repos: repos, users: users, orgs: orgs, cfg: cfg}
}

func (s *RepoService) Create(ctx context.Context, ownerUsername, name, description string, private bool) (*model.Repository, error) {
	owner, err := s.users.GetByUsername(ctx, ownerUsername)
	if err != nil {
		return nil, fmt.Errorf("owner not found: %w", err)
	}

	r := &model.Repository{
		OwnerID:       owner.ID,
		OwnerName:     ownerUsername,
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: "main",
	}
	if err := s.repos.CreateWithOwnerName(ctx, r); err != nil {
		return nil, err
	}

	repoPath := filepath.Join(s.cfg.ReposRoot, ownerUsername, name+".git")
	if _, err := gogit.PlainInit(repoPath, true); err != nil {
		return nil, fmt.Errorf("git init bare: %w", err)
	}

	return r, nil
}

func (s *RepoService) List(ctx context.Context) ([]model.Repository, error) {
	return s.repos.List(ctx)
}

func (s *RepoService) Get(ctx context.Context, owner, name string) (*model.Repository, error) {
	return s.repos.GetByOwnerName(ctx, owner, name)
}

func (s *RepoService) ListByOwner(ctx context.Context, ownerUsername string) ([]model.Repository, error) {
	return s.repos.GetByOwnerNameList(ctx, ownerUsername)
}

// isOrgOwner returns true when the repo belongs to an org and userID is an owner of that org.
func (s *RepoService) isOrgOwner(ctx context.Context, repo *model.Repository, userID int64) bool {
	if repo.OrgID == 0 {
		return false
	}
	m, err := s.orgs.GetMember(ctx, repo.OrgID, userID)
	if err != nil {
		return false
	}
	return m.Role == model.OrgRoleOwner
}

func (s *RepoService) CanRead(ctx context.Context, repo *model.Repository, userID *int64) bool {
	if !repo.Private {
		return true
	}

	if userID == nil {
		return false
	}

	if repo.OwnerID == *userID {
		return true
	}

	if s.isOrgOwner(ctx, repo, *userID) {
		return true
	}

	role, err := s.repos.GetPermission(ctx, repo.ID, *userID)
	if err != nil || role == "" {
		return false
	}

	return true
}

func (s *RepoService) CanWrite(ctx context.Context, repo *model.Repository, userID int64) bool {
	if repo.OwnerID == userID {
		return true
	}
	if s.isOrgOwner(ctx, repo, userID) {
		return true
	}

	role, err := s.repos.GetPermission(ctx, repo.ID, userID)
	if err != nil || role == "" {
		return false
	}

	return role == string(model.RoleWriter) || role == string(model.RoleAdmin)
}

// CanManage returns true only for the repo owner or an org owner.
// Collaborators with admin role can read/write but cannot manage collaborators or settings.
func (s *RepoService) CanManage(ctx context.Context, repo *model.Repository, userID int64) bool {
	if repo.OwnerID == userID {
		return true
	}
	return s.isOrgOwner(ctx, repo, userID)
}

func (s *RepoService) ListCollaborators(ctx context.Context, repoID int64) ([]model.Permission, error) {
	perms, err := s.repos.ListPermissionsWithUsername(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []model.Permission{}
	}
	return perms, nil
}

func (s *RepoService) AddCollaborator(ctx context.Context, repoID int64, username string, role string) error {
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	return s.repos.AddPermission(ctx, repoID, user.ID, role)
}

func (s *RepoService) RemoveCollaborator(ctx context.Context, repoID, userID int64) error {
	return s.repos.RemovePermission(ctx, repoID, userID)
}

// TransferRepo transfers ownership of a personal repo to another user.
// Only the current owner (repo.OwnerID == requestingUserID) may call this.
// Org repos cannot be transferred via this method.
func (s *RepoService) TransferRepo(ctx context.Context, repo *model.Repository, requestingUserID int64, newOwnerUsername string) error {
	if repo.OwnerID != requestingUserID {
		return fmt.Errorf("only the repo owner can transfer ownership")
	}
	if repo.OrgID != 0 {
		return fmt.Errorf("org repos cannot be transferred; manage the org instead")
	}

	newOwner, err := s.users.GetByUsername(ctx, newOwnerUsername)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	if newOwner.ID == requestingUserID {
		return fmt.Errorf("new owner must be a different user")
	}

	oldPath := filepath.Join(s.cfg.ReposRoot, repo.OwnerName, repo.Name+".git")
	newDir := filepath.Join(s.cfg.ReposRoot, newOwnerUsername)
	newPath := filepath.Join(newDir, repo.Name+".git")

	if err := os.MkdirAll(newDir, 0755); err != nil {
		return fmt.Errorf("create owner dir: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("move git dir: %w", err)
	}

	if err := s.repos.UpdateOwner(ctx, repo.ID, newOwner.ID, newOwnerUsername); err != nil {
		// Best-effort rollback of git dir move
		_ = os.Rename(newPath, oldPath)
		return fmt.Errorf("update repo owner: %w", err)
	}
	return nil
}
