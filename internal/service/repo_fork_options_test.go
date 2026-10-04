package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func TestRepoService_Fork_IntoAnOwnedOrgWithNameAndDescription(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	orig, err := env.repos.Create(ctx, ownerID, owner, "upstream", "the original", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := env.repos.AddCollaborator(ctx, orig, ownerID, user, string(model.RoleReader)); err != nil {
		t.Fatalf("grant read: %v", err)
	}
	org := env.createOrg(t, userID)
	desc := "my copy"

	forked, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Owner: org.Name, Name: "downstream", Description: &desc})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	got, err := env.repos.Get(ctx, org.Name, "downstream")
	if err != nil {
		t.Fatalf("fork not found under the org: %v", err)
	}
	if got.ID != forked.ID || got.OrgID != org.ID || got.CreatedBy != userID {
		t.Errorf("fork row = id %d org %d created_by %d, want id %d org %d created_by %d", got.ID, got.OrgID, got.CreatedBy, forked.ID, org.ID, userID)
	}
	if got.Description != desc || !got.Private || !got.IsFork || got.ForkOfID == nil || *got.ForkOfID != orig.ID {
		t.Errorf("fork row = %+v, want description %q, private, fork of %d", got, desc, orig.ID)
	}
	origDir, _ := env.dirs(owner, "upstream")
	forkDir, _ := env.dirs(org.Name, "downstream")
	if headOf(t, forkDir) != headOf(t, origDir) {
		t.Error("fork does not match the original")
	}
	reread, err := env.repos.Get(ctx, owner, "upstream")
	if err != nil {
		t.Fatal(err)
	}
	if reread.ForkCount != 1 {
		t.Errorf("fork count = %d, want 1", reread.ForkCount)
	}
}

func TestRepoService_Fork_RefusesOrgsTheActorDoesNotOwn(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	if _, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create original: %v", err)
	}
	strangers := env.createOrg(t, ownerID)
	joined := env.createOrg(t, ownerID)
	if err := env.orgs.AddMember(ctx, joined.ID, ownerID, userID, model.OrgRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	for _, org := range []*model.Organization{strangers, joined} {
		_, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Owner: org.Name})
		if !errors.Is(err, service.ErrForbidden) {
			t.Errorf("fork into %s: want ErrForbidden, got %v", org.Name, err)
		}
		if n := env.rowCount(t, org.Name, "upstream"); n != 0 {
			t.Errorf("fork into %s left %d rows", org.Name, n)
		}
	}
}

func TestRepoService_Fork_RefusesTheSourcesOwnNamespace(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	if _, err := env.repos.Create(ctx, ownerID, owner, "mine", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create personal repo: %v", err)
	}
	if _, err := env.orgs.WithRepoService(env.repos).CreateRepo(ctx, org.ID, ownerID, "ours", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create org repo: %v", err)
	}

	cases := []struct{ src, into string }{{owner, ""}, {owner, owner}, {org.Name, org.Name}}
	for _, c := range cases {
		name := map[string]string{owner: "mine", org.Name: "ours"}[c.src]
		_, err := env.repos.Fork(ctx, c.src, name, ownerID, owner, service.ForkOptions{Owner: c.into, Name: "copy"})
		if !errors.Is(err, service.ErrForkIntoSourceOwner) {
			t.Errorf("fork %s/%s into %q: want ErrForkIntoSourceOwner, got %v", c.src, name, c.into, err)
		}
	}
	if n := env.rowCount(t, owner, "copy") + env.rowCount(t, org.Name, "copy"); n != 0 {
		t.Errorf("%d copies were created", n)
	}

	if _, err := env.repos.Fork(ctx, org.Name, "ours", ownerID, owner, service.ForkOptions{}); err != nil {
		t.Errorf("fork an org repo into the owner's account: %v", err)
	}
}

func TestRepoService_Fork_ATakenExplicitNameFails(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	if _, err := env.repos.Create(ctx, ownerID, owner, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create original: %v", err)
	}
	if _, err := env.repos.Create(ctx, userID, user, "upstream", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create clash: %v", err)
	}

	_, err := env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Name: "upstream"})
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	if n := env.rowCount(t, user, "upstream-1"); n != 0 {
		t.Error("an explicit name fell back to a suffixed one")
	}

	_, err = env.repos.Fork(ctx, owner, "upstream", userID, user, service.ForkOptions{Name: "../escape"})
	if !errors.Is(err, service.ErrInvalidRepoName) {
		t.Errorf("path as name: want ErrInvalidRepoName, got %v", err)
	}
}
