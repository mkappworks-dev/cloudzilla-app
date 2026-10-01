package service_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type orgRepoOwners struct {
	org                  *model.Organization
	creatorID, coOwnerID int64
	creator              string
	repo                 *model.Repository
}

// orgRepoByCreator has one of two org owners create a private org repo.
func (e repoDirsEnv) orgRepoByCreator(t *testing.T, name string) orgRepoOwners {
	t.Helper()
	ctx := context.Background()
	var o orgRepoOwners
	o.coOwnerID, _ = e.seedUser(t)
	o.creatorID, o.creator = e.seedUser(t)
	o.org = e.createOrg(t, o.coOwnerID)
	if err := e.orgs.AddMember(ctx, o.org.ID, o.coOwnerID, o.creatorID, model.OrgRoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	repo, err := e.orgs.CreateRepo(ctx, o.org.ID, o.creatorID, name, "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	o.repo = repo
	return o
}

func TestRepoService_OrgRepoCreatorLosesOwnerActionsOnLeaving(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "held")
	if err := env.orgs.RemoveMember(ctx, o.org.ID, o.coOwnerID, o.creatorID); err != nil {
		t.Fatalf("remove creator: %v", err)
	}

	if err := env.repos.Archive(ctx, o.repo.ID, o.creatorID); err == nil {
		t.Error("creator outside the org archived the repo")
	}
	if err := env.repos.SetTemplate(ctx, o.repo.ID, o.creatorID, true); err == nil {
		t.Error("creator outside the org made the repo a template")
	}
	if err := env.repos.UpdateVisibility(ctx, o.repo.ID, o.creatorID, false); err == nil {
		t.Error("creator outside the org made the repo public")
	}
	if err := env.repos.Delete(ctx, o.repo.ID, o.creatorID); err == nil {
		t.Error("creator outside the org deleted the repo")
	}
	got, err := env.repos.GetByID(ctx, o.repo.ID)
	if err != nil {
		t.Fatalf("org repo gone: %v", err)
	}
	if got.IsArchived || got.IsTemplate || !got.Private {
		t.Errorf("org repo changed by a creator outside the org: %+v", got)
	}
}

// Restoring is an owner action: the org's owners may, a creator who left may not.
func TestRepoService_Restore_OrgRepoAnswersToOrgOwners(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "binned")
	if err := env.repos.Delete(ctx, o.repo.ID, o.creatorID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := env.orgs.RemoveMember(ctx, o.org.ID, o.coOwnerID, o.creatorID); err != nil {
		t.Fatalf("remove creator: %v", err)
	}

	if err := env.repos.Restore(ctx, o.repo.ID, o.creatorID, false); err == nil {
		t.Fatal("creator outside the org restored the repo")
	}
	if err := env.repos.Restore(ctx, o.repo.ID, o.coOwnerID, false); err != nil {
		t.Fatalf("org owner restoring a repo another owner deleted: %v", err)
	}
	if _, err := env.repos.Get(ctx, o.org.Name, "binned"); err != nil {
		t.Errorf("restored repo not live: %v", err)
	}
}
