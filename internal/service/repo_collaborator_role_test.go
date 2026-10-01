package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newAdminedRepo seeds a personal repo with one admin collaborator.
func newAdminedRepo(t *testing.T, env repoDirsEnv) (repo *model.Repository, adminID int64) {
	t.Helper()
	ownerID, ownerName := env.seedUser(t)
	repoID := testutil.SeedRepo(t, env.db, ownerID, ownerName, testutil.UniqueSuffix(t))
	adminID, _ = env.seedUser(t)
	seedRepoRole(t, env, repoID, adminID, model.RoleAdmin)
	return &model.Repository{ID: repoID, OwnerID: ownerID}, adminID
}

func seedRepoRole(t *testing.T, env repoDirsEnv, repoID, userID int64, role model.Role) {
	t.Helper()
	testutil.Exec(t, env.db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, $3)`, userID, repoID, string(role))
}

func wantRole(t *testing.T, env repoDirsEnv, repoID, userID int64, want model.Role) {
	t.Helper()
	var got model.Role
	if err := env.db.QueryRow(`SELECT COALESCE((SELECT role FROM permissions WHERE repo_id = $1 AND user_id = $2), '')`, repoID, userID).Scan(&got); err != nil {
		t.Fatalf("read role: %v", err)
	}
	if got != want {
		t.Errorf("role = %q, want %q", got, want)
	}
}

// An admin's second account made admin would keep managing the repo after the
// admin is removed.
func TestRepoService_AddCollaborator_AdminCannotAppointAdmin(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	repo, adminID := newAdminedRepo(t, env)
	sockID, sock := env.seedUser(t)

	err := env.repos.AddCollaborator(ctx, repo, adminID, sock, string(model.RoleAdmin))

	if !errors.Is(err, service.ErrAdminRoleOwnerOnly) {
		t.Fatalf("want ErrAdminRoleOwnerOnly, got %v", err)
	}
	if err := env.repos.RemoveCollaborator(ctx, repo, repo.OwnerID, adminID); err != nil {
		t.Fatalf("owner removes admin: %v", err)
	}
	if env.repos.CanManage(ctx, repo, sockID) {
		t.Error("the removed admin's second account still manages the repo")
	}
}

func TestRepoService_AddCollaborator_AdminCannotChangeAdminRole(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	repo, adminID := newAdminedRepo(t, env)
	peerID, peer := env.seedUser(t)
	seedRepoRole(t, env, repo.ID, peerID, model.RoleAdmin)

	err := env.repos.AddCollaborator(ctx, repo, adminID, peer, string(model.RoleWriter))

	if !errors.Is(err, service.ErrAdminRoleOwnerOnly) {
		t.Fatalf("want ErrAdminRoleOwnerOnly, got %v", err)
	}
	wantRole(t, env, repo.ID, peerID, model.RoleAdmin)
}

func TestRepoService_RemoveCollaborator_AdminCannotRemoveAdmin(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	repo, adminID := newAdminedRepo(t, env)
	peerID, _ := env.seedUser(t)
	seedRepoRole(t, env, repo.ID, peerID, model.RoleAdmin)

	err := env.repos.RemoveCollaborator(ctx, repo, adminID, peerID)

	if !errors.Is(err, service.ErrAdminRoleOwnerOnly) {
		t.Fatalf("want ErrAdminRoleOwnerOnly, got %v", err)
	}
	wantRole(t, env, repo.ID, peerID, model.RoleAdmin)
}

// "owner" passes the column's CHECK, but as a collaborator role it grants only read.
func TestRepoService_AddCollaborator_RejectsUnknownRole(t *testing.T) {
	env := newRepoDirsEnv(t)
	repo, _ := newAdminedRepo(t, env)
	for _, role := range []string{"owner", "superadmin", ""} {
		t.Run(role, func(t *testing.T) {
			userID, user := env.seedUser(t)

			err := env.repos.AddCollaborator(context.Background(), repo, repo.OwnerID, user, role)

			if !errors.Is(err, service.ErrInvalidCollaboratorRole) {
				t.Errorf("want ErrInvalidCollaboratorRole, got %v", err)
			}
			wantRole(t, env, repo.ID, userID, "")
		})
	}
}

func TestRepoService_Collaborators_AdminManagesReadersAndWriters(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	repo, adminID := newAdminedRepo(t, env)
	userID, user := env.seedUser(t)

	if err := env.repos.AddCollaborator(ctx, repo, adminID, user, string(model.RoleWriter)); err != nil {
		t.Fatalf("add writer: %v", err)
	}
	if err := env.repos.AddCollaborator(ctx, repo, adminID, user, string(model.RoleReader)); err != nil {
		t.Fatalf("demote to reader: %v", err)
	}
	wantRole(t, env, repo.ID, userID, model.RoleReader)
	if err := env.repos.RemoveCollaborator(ctx, repo, adminID, userID); err != nil {
		t.Fatalf("remove reader: %v", err)
	}
	wantRole(t, env, repo.ID, userID, "")
}

func TestRepoService_Collaborators_OwnerManagesAdmins(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	repo, adminID := newAdminedRepo(t, env)
	userID, user := env.seedUser(t)

	if err := env.repos.AddCollaborator(ctx, repo, repo.OwnerID, user, string(model.RoleAdmin)); err != nil {
		t.Fatalf("appoint admin: %v", err)
	}
	wantRole(t, env, repo.ID, userID, model.RoleAdmin)
	if err := env.repos.RemoveCollaborator(ctx, repo, repo.OwnerID, adminID); err != nil {
		t.Fatalf("remove admin: %v", err)
	}
	wantRole(t, env, repo.ID, adminID, "")
}

// An org member made repo admin manages the repo but appoints no admins; org
// owners do.
func TestRepoService_AddCollaborator_OrgRepo_OnlyOrgOwnersAppointAdmins(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, _ := env.seedUser(t)
	memberID, member := env.seedUser(t)
	recruitID, recruit := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	if err := env.orgs.AddMember(ctx, org.ID, ownerID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	repo, err := env.orgs.CreateRepo(ctx, org.ID, ownerID, "vault", "", true, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create org repo: %v", err)
	}
	if err := env.repos.AddCollaborator(ctx, repo, ownerID, member, string(model.RoleAdmin)); err != nil {
		t.Fatalf("org owner appoints admin: %v", err)
	}

	err = env.repos.AddCollaborator(ctx, repo, memberID, recruit, string(model.RoleAdmin))

	if !errors.Is(err, service.ErrAdminRoleOwnerOnly) {
		t.Fatalf("want ErrAdminRoleOwnerOnly, got %v", err)
	}
	wantRole(t, env, repo.ID, recruitID, "")
}
