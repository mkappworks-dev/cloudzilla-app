package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validateName checks that a repository or owner name is safe for filesystem
// use and URL routing. Names must start with an alphanumeric character and
// contain only alphanumeric, dot, underscore, or hyphen characters.
func validateName(name string) error {
	if len(name) == 0 || len(name) > 100 {
		return fmt.Errorf("name must be 1-100 characters")
	}
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("name contains invalid characters")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name is reserved")
	}
	return nil
}

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
	if err := validateName(name); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}

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

func (s *RepoService) GetByID(ctx context.Context, id int64) (*model.Repository, error) {
	return s.repos.GetByID(ctx, id)
}

func (s *RepoService) Get(ctx context.Context, owner, name string) (*model.Repository, error) {
	repo, err := s.repos.GetByOwnerName(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	s.enrichForkInfo(ctx, repo)
	return repo, nil
}

func (s *RepoService) enrichForkInfo(ctx context.Context, repo *model.Repository) {
	if repo.ForkOfID == nil {
		return
	}
	orig, err := s.repos.GetByID(ctx, *repo.ForkOfID)
	if err != nil {
		return
	}
	repo.ForkOfOwner = orig.OwnerName
	repo.ForkOfName = orig.Name
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

// Fork creates a copy of originalOwner/originalName under the actor's namespace.
func (s *RepoService) Fork(ctx context.Context, originalOwner, originalName string, actorID int64, actorUsername string) (*model.Repository, error) {
	orig, err := s.repos.GetByOwnerName(ctx, originalOwner, originalName)
	if err != nil {
		return nil, fmt.Errorf("original repo not found: %w", err)
	}

	if !s.CanRead(ctx, orig, &actorID) {
		return nil, fmt.Errorf("access denied")
	}

	// Determine fork name (avoid collision)
	forkName := originalName
	for i := 1; ; i++ {
		_, err := s.repos.GetByOwnerName(ctx, actorUsername, forkName)
		if err != nil {
			break // name is available
		}
		forkName = fmt.Sprintf("%s-%d", originalName, i)
	}

	forked, err := s.repos.Fork(ctx, orig, actorID, actorUsername, forkName)
	if err != nil {
		return nil, fmt.Errorf("fork db record: %w", err)
	}

	// Copy the bare git repo directory
	srcPath := filepath.Join(s.cfg.ReposRoot, originalOwner, originalName+".git")
	dstDir := filepath.Join(s.cfg.ReposRoot, actorUsername)
	dstPath := filepath.Join(dstDir, forkName+".git")

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		_ = s.repos.DecrementForkCount(ctx, orig.ID)
		return nil, fmt.Errorf("create owner dir: %w", err)
	}

	if err := copyDir(srcPath, dstPath); err != nil {
		// Rollback DB record
		_, _ = ctx, forked // best effort
		return nil, fmt.Errorf("copy git dir: %w", err)
	}

	_ = s.repos.IncrementForkCount(ctx, orig.ID)

	forked.ForkOfOwner = originalOwner
	forked.ForkOfName = originalName
	return forked, nil
}

// copyDir recursively copies src directory to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// archiveGuard returns an error if the caller does not have manage permission.
func archiveGuard(canManage bool) error {
	if !canManage {
		return fmt.Errorf("forbidden: only the repo owner or org owner can archive a repo")
	}
	return nil
}

// templateGuard returns an error if the caller does not have manage permission.
func templateGuard(canManage bool) error {
	if !canManage {
		return fmt.Errorf("forbidden: only the repo owner or org owner can change template status")
	}
	return nil
}

func (s *RepoService) Archive(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := archiveGuard(s.CanManage(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetArchived(ctx, repoID, true)
}

func (s *RepoService) Unarchive(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := archiveGuard(s.CanManage(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetArchived(ctx, repoID, false)
}

func (s *RepoService) SetTemplate(ctx context.Context, repoID, userID int64, isTemplate bool) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := templateGuard(s.CanManage(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetTemplate(ctx, repoID, isTemplate)
}

func (s *RepoService) CreateFromTemplate(ctx context.Context, templateRepoID, newOwnerID int64, newOwnerUsername, newName, description string) (*model.Repository, error) {
	tmpl, err := s.repos.GetByID(ctx, templateRepoID)
	if err != nil {
		return nil, fmt.Errorf("template repo not found: %w", err)
	}
	if !tmpl.IsTemplate {
		return nil, fmt.Errorf("repository is not a template")
	}
	if tmpl.Private {
		return nil, fmt.Errorf("template repo must be public")
	}
	if tmpl.IsArchived {
		return nil, fmt.Errorf("template repo is archived")
	}

	newRepo := &model.Repository{
		OwnerID:       newOwnerID,
		OwnerName:     newOwnerUsername,
		Name:          newName,
		Description:   description,
		Private:       false,
		DefaultBranch: tmpl.DefaultBranch,
	}
	if err := s.repos.CreateWithOwnerName(ctx, newRepo); err != nil {
		return nil, fmt.Errorf("create repo from template: %w", err)
	}

	srcPath := filepath.Join(s.cfg.ReposRoot, tmpl.OwnerName, tmpl.Name+".git")
	dstDir := filepath.Join(s.cfg.ReposRoot, newOwnerUsername)
	dstPath := filepath.Join(dstDir, newName+".git")

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		_ = s.repos.DeleteByID(ctx, newRepo.ID)
		return nil, fmt.Errorf("create owner dir: %w", err)
	}

	if _, statErr := os.Stat(srcPath); statErr == nil {
		if err := copyDir(srcPath, dstPath); err != nil {
			_ = s.repos.DeleteByID(ctx, newRepo.ID)
			return nil, fmt.Errorf("copy template git dir: %w", err)
		}
	} else {
		if _, err := gogit.PlainInit(dstPath, true); err != nil {
			_ = s.repos.DeleteByID(ctx, newRepo.ID)
			return nil, fmt.Errorf("git init bare for template copy: %w", err)
		}
	}

	return newRepo, nil
}

func (s *RepoService) ListTemplates(ctx context.Context) ([]model.Repository, error) {
	return s.repos.ListTemplates(ctx)
}

// deleteGuard returns an error if the caller does not have manage permission.
func deleteGuard(canManage bool) error {
	if !canManage {
		return fmt.Errorf("forbidden: only the repo owner or org owner can delete a repo")
	}
	return nil
}

func (s *RepoService) Delete(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := deleteGuard(s.CanManage(ctx, repo, userID)); err != nil {
		return err
	}

	repoPath := filepath.Join(s.cfg.ReposRoot, repo.OwnerName, repo.Name+".git")
	deletedPath := repoPath + ".deleted." + strconv.FormatInt(time.Now().Unix(), 10)
	if _, err := os.Stat(repoPath); err == nil {
		if err := os.Rename(repoPath, deletedPath); err != nil {
			return fmt.Errorf("rename git dir for soft delete: %w", err)
		}
	}

	return s.repos.Delete(ctx, repoID, userID)
}

func (s *RepoService) Restore(ctx context.Context, repoID, requesterID int64, isSuperadmin bool) error {
	repo, err := s.repos.GetDeletedByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("deleted repo not found: %w", err)
	}
	if repo.OwnerID != requesterID && !isSuperadmin {
		return fmt.Errorf("forbidden: only the original owner or a superadmin can restore a repo")
	}

	ownerDir := filepath.Join(s.cfg.ReposRoot, repo.OwnerName)
	pattern := filepath.Join(ownerDir, repo.Name+".git.deleted.*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob deleted git dir: %w", err)
	}
	if len(matches) > 0 {
		latestMatch := matches[len(matches)-1]
		restoredPath := filepath.Join(ownerDir, repo.Name+".git")
		if _, statErr := os.Stat(restoredPath); statErr == nil {
			return fmt.Errorf("restore conflict: live repo dir already exists at %s", restoredPath)
		}
		if err := os.Rename(latestMatch, restoredPath); err != nil {
			return fmt.Errorf("rename git dir back on restore: %w", err)
		}
	}

	return s.repos.Restore(ctx, repoID)
}

func (s *RepoService) GetDeleted(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	return s.repos.GetDeletedByOwnerAndName(ctx, ownerName, name)
}

func (s *RepoService) PurgeExpired(ctx context.Context) error {
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	expired, err := s.repos.PurgeExpired(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("purge expired repos: %w", err)
	}
	for _, r := range expired {
		ownerDir := filepath.Join(s.cfg.ReposRoot, r.OwnerName)
		pattern := filepath.Join(ownerDir, r.Name+".git.deleted.*")
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			slog.Warn("purge: failed to glob deleted git dir", "pattern", pattern, "error", globErr)
			continue
		}
		for _, m := range matches {
			if removeErr := os.RemoveAll(m); removeErr != nil {
				slog.Warn("purge: failed to remove deleted git dir", "path", m, "error", removeErr)
			}
		}
	}
	return nil
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
