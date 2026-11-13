package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/adminmode"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func (s *RepoService) isOrgOwner(ctx context.Context, orgID, userID int64) bool {
	if orgID == 0 {
		return false
	}
	m, err := s.orgs.GetMember(ctx, orgID, userID)
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

	if s.ownsOrdinarily(ctx, repo, *userID) {
		return true
	}

	if role, err := s.repos.EffectiveRole(ctx, repo.ID, *userID); err == nil && role != "" {
		return true
	}

	return adminmode.Override(ctx, *userID)
}

func (s *RepoService) CanWrite(ctx context.Context, repo *model.Repository, userID int64) bool {
	if s.ownsOrdinarily(ctx, repo, userID) {
		return true
	}

	role, err := s.repos.EffectiveRole(ctx, repo.ID, userID)
	if err == nil && (role == string(model.RoleWriter) || role == string(model.RoleAdmin)) {
		return true
	}

	return adminmode.Override(ctx, userID)
}

// CanManage returns true for the repo owner, org owner, or an admin by collaborator role or team grant.
// Grants access to manage collaborators, settings, branch protection, deploy keys, etc.
// Does NOT grant transfer, delete or the admin role — use IsOwner for those.
func (s *RepoService) CanManage(ctx context.Context, repo *model.Repository, userID int64) bool {
	if s.ownsOrdinarily(ctx, repo, userID) {
		return true
	}
	if role, err := s.repos.EffectiveRole(ctx, repo.ID, userID); err == nil && role == string(model.RoleAdmin) {
		return true
	}
	return adminmode.Override(ctx, userID)
}

// ListManagers returns the ids of the users CanManage admits on repo, except a superadmin passing only through admin mode.
func (s *RepoService) ListManagers(ctx context.Context, repo *model.Repository) ([]int64, error) {
	return s.repos.ListManagerIDs(ctx, repo)
}

// IsOwner returns true only for the owner of a personal repo or an owner of
// an org repo's org; having created an org repo counts for nothing.
// Used for destructive operations: transfer, delete, archive, unarchive, template toggle,
// and for granting, changing or removing the admin role.
// A superadmin in admin mode passes too, for themselves only.
func (s *RepoService) IsOwner(ctx context.Context, repo *model.Repository, userID int64) bool {
	return s.ownsOrdinarily(ctx, repo, userID) || adminmode.Override(ctx, userID)
}

// ownsOrdinarily is IsOwner without admin mode, so the checks built on it ask
// for the override only after every ordinary grant has failed.
func (s *RepoService) ownsOrdinarily(ctx context.Context, repo *model.Repository, userID int64) bool {
	if repo.OrgID != 0 {
		return s.isOrgOwner(ctx, repo.OrgID, userID)
	}
	return repo.OwnerID == userID
}

func (s *RepoService) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.Permission, error) {
	return s.repos.ListPermissionsByUser(ctx, userID)
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

var (
	ErrInvalidCollaboratorRole = errors.New("role must be reader, writer or admin")
	ErrAdminRoleOwnerOnly      = errors.New("only the repository owner can grant, change or remove the admin role")
)

// AddCollaborator grants username role on repo, or changes the role they hold.
func (s *RepoService) AddCollaborator(ctx context.Context, repo *model.Repository, requestingUserID int64, username string, role string) error {
	switch model.Role(role) {
	case model.RoleReader, model.RoleWriter, model.RoleAdmin:
	default:
		return ErrInvalidCollaboratorRole
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	if err := s.checkAdminRoleChange(ctx, repo, requestingUserID, user.ID, model.Role(role)); err != nil {
		return err
	}
	return s.repos.AddPermission(ctx, repo.ID, user.ID, role)
}

func (s *RepoService) RemoveCollaborator(ctx context.Context, repo *model.Repository, requestingUserID, userID int64) error {
	if err := s.checkAdminRoleChange(ctx, repo, requestingUserID, userID, ""); err != nil {
		return err
	}
	return s.repos.RemovePermission(ctx, repo.ID, userID)
}

// checkAdminRoleChange lets only an owner give userID the admin role, or change
// or remove one userID holds; an empty newRole is a removal. An admin who could
// appoint admins could plant a second account that outlives their own removal.
func (s *RepoService) checkAdminRoleChange(ctx context.Context, repo *model.Repository, requestingUserID, userID int64, newRole model.Role) error {
	if s.IsOwner(ctx, repo, requestingUserID) {
		return nil
	}
	if newRole == model.RoleAdmin {
		return ErrAdminRoleOwnerOnly
	}
	current, err := s.repos.GetPermission(ctx, repo.ID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if model.Role(current) == model.RoleAdmin {
		return ErrAdminRoleOwnerOnly
	}
	return nil
}
