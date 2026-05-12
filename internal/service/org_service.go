package service

import (
	"context"
	"fmt"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// OrgService manages organization creation, membership, and ownership transfers.
type OrgService struct {
	orgs  *store.OrgStore
	repos *store.RepoStore
	users *store.UserStore
	cfg   config.GitConfig
}

// NewOrgService creates an OrgService backed by the given stores and git config.
func NewOrgService(orgs *store.OrgStore, repos *store.RepoStore, users *store.UserStore, cfg config.GitConfig) *OrgService {
	return &OrgService{orgs: orgs, repos: repos, users: users, cfg: cfg}
}

func (s *OrgService) Create(ctx context.Context, creatorUserID int64, name, displayName, description string) (*model.Organization, error) {
	// Check name not already used by a user
	if _, err := s.users.GetByUsername(ctx, name); err == nil {
		return nil, fmt.Errorf("name already taken by a user account")
	}

	org := &model.Organization{
		Name:        name,
		DisplayName: displayName,
		Description: description,
	}
	if err := s.orgs.Create(ctx, org); err != nil {
		return nil, fmt.Errorf("create org: %w", err)
	}

	if err := s.orgs.AddMember(ctx, org.ID, creatorUserID, model.OrgRoleOwner); err != nil {
		return nil, fmt.Errorf("add org owner: %w", err)
	}

	return org, nil
}

func (s *OrgService) Get(ctx context.Context, name string) (*model.Organization, error) {
	return s.orgs.GetByName(ctx, name)
}

func (s *OrgService) ListMembers(ctx context.Context, orgID int64) ([]model.OrgMember, error) {
	return s.orgs.ListMembers(ctx, orgID)
}

func (s *OrgService) IsOwner(ctx context.Context, orgID, userID int64) bool {
	m, err := s.orgs.GetMember(ctx, orgID, userID)
	if err != nil {
		return false
	}
	return m.Role == model.OrgRoleOwner
}

func (s *OrgService) IsMember(ctx context.Context, orgID, userID int64) bool {
	_, err := s.orgs.GetMember(ctx, orgID, userID)
	return err == nil
}

func (s *OrgService) AddMember(ctx context.Context, orgID, requestingUserID, targetUserID int64, role model.OrgRole) error {
	if !s.IsOwner(ctx, orgID, requestingUserID) {
		return fmt.Errorf("only org owners can add members")
	}
	return s.orgs.AddMember(ctx, orgID, targetUserID, role)
}

func (s *OrgService) RemoveMember(ctx context.Context, orgID, requestingUserID, targetUserID int64) error {
	if !s.IsOwner(ctx, orgID, requestingUserID) {
		return fmt.Errorf("only org owners can remove members")
	}

	// Prevent removing last owner
	members, err := s.orgs.ListMembers(ctx, orgID)
	if err != nil {
		return err
	}
	ownerCount := 0
	for _, m := range members {
		if m.Role == model.OrgRoleOwner {
			ownerCount++
		}
	}

	targetMember, err := s.orgs.GetMember(ctx, orgID, targetUserID)
	if err != nil {
		return fmt.Errorf("member not found")
	}
	if targetMember.Role == model.OrgRoleOwner && ownerCount <= 1 {
		return fmt.Errorf("cannot remove the last owner")
	}

	return s.orgs.RemoveMember(ctx, orgID, targetUserID)
}

func (s *OrgService) CreateRepo(ctx context.Context, orgID, requestingUserID int64, name, description string, private bool) (*model.Repository, error) {
	if err := ValidateName(name); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}
	if !s.IsOwner(ctx, orgID, requestingUserID) {
		return nil, fmt.Errorf("only org owners can create repos")
	}

	org, err := s.orgs.GetByID(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("org not found: %w", err)
	}

	r := &model.Repository{
		OwnerID:       requestingUserID,
		OwnerName:     org.Name,
		OrgID:         orgID,
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: "main",
	}
	if err := s.repos.CreateWithOwnerName(ctx, r); err != nil {
		return nil, fmt.Errorf("create org repo: %w", err)
	}

	repoPath := filepath.Join(s.cfg.ReposRoot, org.Name, name+".git")
	if _, err := gogit.PlainInit(repoPath, true); err != nil {
		return nil, fmt.Errorf("git init bare: %w", err)
	}

	return r, nil
}

func (s *OrgService) ListRepos(ctx context.Context, orgID int64) ([]model.Repository, error) {
	return s.repos.GetByOrgID(ctx, orgID)
}

// ListReposVisibleTo returns the org's repositories filtered to those the
// viewer is allowed to see. Org owners see all repos; everyone else sees
// public repos plus private repos where they have a collaborator role.
// Pass nil for viewerID for anonymous viewers.
func (s *OrgService) ListReposVisibleTo(ctx context.Context, orgID int64, viewerID *int64) ([]model.Repository, error) {
	repos, err := s.repos.GetByOrgID(ctx, orgID)
	if err != nil {
		return nil, err
	}
	isOrgOwner := false
	if viewerID != nil {
		isOrgOwner = s.IsOwner(ctx, orgID, *viewerID)
	}
	visible := make([]model.Repository, 0, len(repos))
	for _, repo := range repos {
		switch {
		case !repo.Private, isOrgOwner:
			visible = append(visible, repo)
		case viewerID == nil:
			// anonymous viewer; private repo hidden
		default:
			if role, err := s.repos.GetPermission(ctx, repo.ID, *viewerID); err == nil && role != "" {
				visible = append(visible, repo)
			}
		}
	}
	return visible, nil
}

// TransferOrg transfers ownership of an org from the requesting user to another user.
// The requesting user must be an owner. They are demoted to member; the new user becomes owner.
func (s *OrgService) TransferOrg(ctx context.Context, orgID, requestingUserID int64, newOwnerUsername string) error {
	if !s.IsOwner(ctx, orgID, requestingUserID) {
		return fmt.Errorf("only org owners can transfer ownership")
	}

	newOwner, err := s.users.GetByUsername(ctx, newOwnerUsername)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	if newOwner.ID == requestingUserID {
		return fmt.Errorf("new owner must be a different user")
	}

	// Promote new owner (add or update role)
	if _, err := s.orgs.GetMember(ctx, orgID, newOwner.ID); err == nil {
		if err := s.orgs.UpdateMemberRole(ctx, orgID, newOwner.ID, model.OrgRoleOwner); err != nil {
			return fmt.Errorf("promote new owner: %w", err)
		}
	} else {
		if err := s.orgs.AddMember(ctx, orgID, newOwner.ID, model.OrgRoleOwner); err != nil {
			return fmt.Errorf("add new owner: %w", err)
		}
	}

	// Demote requesting user to member
	if err := s.orgs.UpdateMemberRole(ctx, orgID, requestingUserID, model.OrgRoleMember); err != nil {
		return fmt.Errorf("demote old owner: %w", err)
	}
	return nil
}
