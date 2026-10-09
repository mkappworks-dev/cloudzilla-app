package service_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// fullUser is a user at a 1-repo quota that already holds that repo.
func (e quotaEnv) fullUser(t *testing.T) (id int64, name string, held *model.Repository) {
	t.Helper()
	id, name = e.seedUser(t)
	held, err := e.repos.Create(context.Background(), id, name, "held", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create the repo that fills the quota: %v", err)
	}
	return id, name, held
}

var oneRepoEach = config.QuotaConfig{
	User: config.QuotaLimits{Repos: 1},
	Org:  config.QuotaLimits{Repos: 1},
}

func TestQuotaPaths_CreateIsRefusedAtTheCount(t *testing.T) {
	e := newQuotaEnv(t, oneRepoEach)
	id, name, _ := e.fullUser(t)

	_, err := e.repos.Create(context.Background(), id, name, "second", "", false, service.RepoInitOptions{})

	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
	if gitDir, _ := e.dirs(name, "second"); pathExists(gitDir) {
		t.Error("a refused create must not leave a directory behind")
	}
}

func TestQuotaPaths_ForkIsRefusedAtTheTargetsCount(t *testing.T) {
	e := newQuotaEnv(t, oneRepoEach)
	ctx := context.Background()
	srcID, srcName := e.seedUser(t)
	if _, err := e.repos.Create(ctx, srcID, srcName, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatal(err)
	}
	id, name, _ := e.fullUser(t)

	_, err := e.repos.Fork(ctx, srcName, "upstream", id, name, service.ForkOptions{})

	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
}

func TestQuotaPaths_ForkIntoAnOrgCountsTheOrgsRepos(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{Org: config.QuotaLimits{Repos: 1}})
	ctx := context.Background()
	srcID, srcName := e.seedUser(t)
	if _, err := e.repos.Create(ctx, srcID, srcName, "upstream", "", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatal(err)
	}
	actorID, actor := e.seedUser(t)
	_, org := e.seedOrg(t, actorID)

	if _, err := e.repos.Fork(ctx, srcName, "upstream", actorID, actor, service.ForkOptions{Owner: org}); err != nil {
		t.Fatalf("first fork into the org: %v", err)
	}
	_, err := e.repos.Fork(ctx, srcName, "upstream", actorID, actor, service.ForkOptions{Owner: org, Name: "again"})

	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
}

func TestQuotaPaths_TemplateCreateIsRefusedAtTheCount(t *testing.T) {
	e := newQuotaEnv(t, oneRepoEach)
	ctx := context.Background()
	tmplOwnerID, tmplOwner := e.seedUser(t)
	tmpl, err := e.repos.Create(ctx, tmplOwnerID, tmplOwner, "tmpl", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.repos.SetTemplate(ctx, tmpl.ID, tmplOwnerID, true); err != nil {
		t.Fatal(err)
	}
	id, name, _ := e.fullUser(t)

	_, err = e.repos.CreateFromTemplate(ctx, tmpl.ID, id, name, "fromtmpl", "")

	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
}

func TestQuotaPaths_ImportIsRefusedAtTheCountWhenItStartsAndWhenItPublishes(t *testing.T) {
	e := newQuotaEnv(t, oneRepoEach)
	ctx := context.Background()
	id, name, _ := e.fullUser(t)
	target := service.RepoTarget{ActorID: id, OwnerName: name, OwnerID: id}

	imports := service.NewImportService(e.repos, config.GitConfig{ReposRoot: e.root}, config.ImportConfig{})
	_, err := imports.Start(ctx, id, name, service.ImportRequest{CloneURL: "https://example.com/acme/app.git", Name: "imported"})
	wantQuotaErr(t, err, "repository quota reached (1 of 1)")

	src := t.TempDir()
	testutil.InitBareRepo(t, src)
	_, err = e.repos.CreateFromImport(ctx, target, "imported", "", false, "main", src)
	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
}

func TestQuotaPaths_TransferIsRefusedWhenTheNewOwnerIsFullAtOfferAndAtAccept(t *testing.T) {
	e := newQuotaEnv(t, oneRepoEach)
	ctx := context.Background()
	senderID, sender := e.seedUser(t)
	gift := e.personalRepo(t, senderID, sender, "gift")
	_, full, _ := e.fullUser(t)

	_, err := e.repos.TransferRepo(ctx, gift, senderID, full)
	wantQuotaErr(t, err, "repository quota reached (1 of 1)")

	roomyID, roomy := e.seedUser(t)
	offer := e.offer(t, gift, senderID, roomy)
	if _, err := e.repos.Create(ctx, roomyID, roomy, "filler", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = e.repos.AcceptTransfer(ctx, offer.ID, roomyID, offer.FullName())
	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
	e.wantUnder(t, gift.ID, sender)
}

func TestQuotaPaths_TransferToAnOrgIsRefusedWhenTheOrgIsFull(t *testing.T) {
	e := newQuotaEnv(t, oneRepoEach)
	ctx := context.Background()
	ownerID, owner := e.seedUser(t)
	orgID, org := e.seedOrg(t, ownerID)
	if _, err := e.orgs.CreateRepo(ctx, orgID, ownerID, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}
	// owner has one repo of their own, which a 1-repo user quota allows.
	moving := e.personalRepo(t, ownerID, owner, "moving")

	_, err := e.repos.TransferRepo(ctx, moving, ownerID, org)

	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
	e.wantUnder(t, moving.ID, owner)
}

func TestQuotaPaths_RestoreIsRefusedAtTheCount(t *testing.T) {
	e := newQuotaEnv(t, oneRepoEach)
	ctx := context.Background()
	id, name, held := e.fullUser(t)
	if err := e.repos.Delete(ctx, held.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repos.Create(ctx, id, name, "replacement", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}

	err := e.repos.Restore(ctx, held.ID, id, false)

	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
}

func TestQuotaPaths_AllowedWhenUnderTheCount(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{Repos: 3}})
	id, name, _ := e.fullUser(t)

	if _, err := e.repos.Create(context.Background(), id, name, "second", "", false, service.RepoInitOptions{}); err != nil {
		t.Errorf("1 of 3 used: %v", err)
	}
}
