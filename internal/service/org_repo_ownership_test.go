package service_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (e repoDirsEnv) coOwnedOrg(t *testing.T, ownerID, coOwnerID int64) *model.Organization {
	t.Helper()
	org := e.createOrg(t, ownerID)
	if err := e.orgs.AddMember(context.Background(), org.ID, ownerID, coOwnerID, model.OrgRoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	return org
}

// race runs both at once, each after a random delay of up to jitter so that
// repeated rounds cover different interleavings.
func race(jitter time.Duration, a, b func()) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, f := range []func(){a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			time.Sleep(rand.N(jitter))
			f()
		}()
	}
	close(start)
	wg.Wait()
}

func TestRepoService_Transfer_PersonalRepoIntoOwnedOrg(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	coOwnerID, _ := env.seedUser(t)
	org := env.coOwnedOrg(t, userID, coOwnerID)
	repo, err := env.repos.Create(ctx, userID, user, "moving", "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := env.code.WikiPageSave(user, "moving", "Home", "moving wiki", dirsTestAuthor, ""); err != nil {
		t.Fatalf("save wiki: %v", err)
	}
	oldGit, _ := env.dirs(user, "moving")
	head := headOf(t, oldGit)

	if transfer, err := env.repos.TransferRepo(ctx, repo, userID, org.Name); err != nil || transfer != nil {
		t.Fatalf("TransferRepo into an owned org = %+v, %v; want it moved at once", transfer, err)
	}

	got, err := env.repos.Get(ctx, org.Name, "moving")
	if err != nil {
		t.Fatalf("Get under the org: %v", err)
	}
	if got.OwnerID != 0 || got.OrgID != org.ID || got.CreatedBy != userID {
		t.Errorf("transferred repo: owner_id %d org_id %d created_by %d; want 0, %d, %d", got.OwnerID, got.OrgID, got.CreatedBy, org.ID, userID)
	}
	env.wantLive(t, org.Name, "moving", head, "moving wiki")
	if pathExists(oldGit) {
		t.Error("repo dir left at the personal path")
	}
	if !env.repos.IsOwner(ctx, got, coOwnerID) {
		t.Error("the org's other owner does not own the transferred repo")
	}
	if err := env.orgs.UpdateMemberRole(ctx, org.ID, coOwnerID, userID, model.OrgRoleMember); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if uid := userID; env.repos.CanRead(ctx, got, &uid) {
		t.Error("former personal owner can still read the private org repo as a plain member")
	}
}

func TestRepoService_Transfer_IntoOrgNeedsItsOwnership(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	ownerID, _ := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	if err := env.orgs.AddMember(ctx, org.ID, ownerID, userID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	repo, err := env.repos.Create(ctx, userID, user, "stay", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := env.repos.TransferRepo(ctx, repo, userID, org.Name); err == nil {
		t.Fatal("a plain member transferred a repo into the org")
	}
	if got, err := env.repos.Get(ctx, user, "stay"); err != nil || got.OwnerID != userID || got.OrgID != 0 {
		t.Errorf("repo row changed by a refused transfer: %+v, %v", got, err)
	}
	if gitDir, _ := env.dirs(org.Name, "stay"); pathExists(gitDir) {
		t.Error("repo dir moved into the org by a refused transfer")
	}
}

func TestRepoService_Transfer_OrgRepoToUserNeedsOrgOwnership(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "leaving")
	memberID, member := env.seedUser(t)
	if err := env.orgs.AddMember(ctx, o.org.ID, o.coOwnerID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	heirID, heir := env.seedUser(t)
	gitDir, _ := env.dirs(o.org.Name, "leaving")
	head := headOf(t, gitDir)

	if _, err := env.repos.TransferRepo(ctx, o.repo, memberID, member); err == nil {
		t.Error("a plain member moved an org repo into their account")
	}
	if err := env.orgs.UpdateMemberRole(ctx, o.org.ID, o.coOwnerID, o.creatorID, model.OrgRoleMember); err != nil {
		t.Fatalf("demote creator: %v", err)
	}
	if _, err := env.repos.TransferRepo(ctx, o.repo, o.creatorID, o.creator); err == nil {
		t.Error("the demoted creator moved the org repo into their account")
	}

	if err := env.transferTo(t, o.repo, o.coOwnerID, heir); err != nil {
		t.Fatalf("transfer by an org owner: %v", err)
	}
	got, err := env.repos.Get(ctx, heir, "leaving")
	if err != nil {
		t.Fatalf("Get under the heir: %v", err)
	}
	if got.OwnerID != heirID || got.OrgID != 0 {
		t.Errorf("transferred repo: owner_id %d org_id %d; want %d and 0", got.OwnerID, got.OrgID, heirID)
	}
	if heirGit, _ := env.dirs(heir, "leaving"); headOf(t, heirGit) != head {
		t.Error("repo history did not move to the heir")
	}
	if pathExists(gitDir) {
		t.Error("repo dir left under the org")
	}
	if env.repos.IsOwner(ctx, got, o.coOwnerID) {
		t.Error("org owner still owns a repo that left the org")
	}
}

func TestRepoService_Transfer_RefusesTheCurrentOwnerAndATakenName(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "dup")
	if _, err := env.repos.TransferRepo(ctx, o.repo, o.coOwnerID, o.org.Name); err == nil {
		t.Error("transferred an org repo to its own org")
	}

	userID, user := env.seedUser(t)
	if err := env.orgs.AddMember(ctx, o.org.ID, o.coOwnerID, userID, model.OrgRoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	mine, err := env.repos.Create(ctx, userID, user, "dup", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := env.repos.TransferRepo(ctx, mine, userID, o.org.Name); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	if got, err := env.repos.Get(ctx, user, "dup"); err != nil || got.OwnerID != userID {
		t.Errorf("personal repo moved by a refused transfer: %+v, %v", got, err)
	}
	if n := env.rowCount(t, o.org.Name, "dup"); n != 1 {
		t.Errorf("want 1 row for %s/dup, got %d", o.org.Name, n)
	}
}

// Two owners can act on one org repo; the one acting on an old read must fail
// rather than point the row somewhere the directory is not.
func TestRepoService_Transfer_RefusesAStaleRead(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "raced")
	stale := *o.repo
	_, first := env.seedUser(t)
	_, second := env.seedUser(t)
	if err := env.transferTo(t, o.repo, o.coOwnerID, first); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	firstGit, _ := env.dirs(first, "raced")
	head := headOf(t, firstGit)

	if _, err := env.repos.TransferRepo(ctx, &stale, o.coOwnerID, second); err == nil {
		t.Fatal("a transfer from a stale read succeeded")
	}
	if n := env.rowCount(t, first, "raced"); n != 1 {
		t.Errorf("want the repo row still under %s, got %d rows", first, n)
	}
	if _, err := env.repos.Get(ctx, first, "raced"); err != nil {
		t.Errorf("row moved away from %s: %v", first, err)
	}
	if headOf(t, firstGit) != head {
		t.Error("repo dir changed by the refused transfer")
	}
}

func TestRepoService_Transfer_RefusesADeletedRepo(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "binned")
	stale := *o.repo
	if err := env.repos.Delete(ctx, o.repo.ID, o.coOwnerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, heir := env.seedUser(t)

	if _, err := env.repos.TransferRepo(ctx, &stale, o.coOwnerID, heir); err == nil {
		t.Fatal("transferred a soft-deleted repo")
	}
	if err := env.repos.Restore(ctx, o.repo.ID, o.coOwnerID, false); err != nil {
		t.Fatalf("org owner can no longer restore the repo: %v", err)
	}
	if _, err := env.repos.Get(ctx, o.org.Name, "binned"); err != nil {
		t.Errorf("restored repo not live under the org: %v", err)
	}
}

// The org_id cascade would hard-delete a repo that lands between the empty
// check and the delete.
func TestOrgService_DeleteRacingATransferIn_KeepsTheRepo(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	orgs := env.orgs.WithRepoService(env.repos)
	for round := 0; round < 60; round++ {
		userID, user := env.seedUser(t)
		org := env.createOrg(t, userID)
		repo, err := env.repos.Create(ctx, userID, user, "precious", "", false, service.RepoInitOptions{AddREADME: true})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		race(5*time.Millisecond,
			func() { _ = orgs.Delete(ctx, org.ID, userID) },
			func() { _, _ = env.repos.TransferRepo(ctx, repo, userID, org.Name) },
		)
		var ownerName string
		if err := env.db.QueryRow(`SELECT owner_name FROM repositories WHERE id = $1`, repo.ID).Scan(&ownerName); err != nil {
			t.Fatalf("round %d: repo row gone: %v", round, err)
		}
		if gitDir, _ := env.dirs(ownerName, "precious"); !pathExists(gitDir) {
			t.Fatalf("round %d: the row names %s, which has no repo dir", round, ownerName)
		}
	}
}

// Account deletion must lock the orgs the user is only a member of too: a
// promotion landing mid-delete would otherwise count them as the owner who stays.
func TestOrgService_PromotingAMemberWhoIsDeletingTheirAccount_KeepsAnOwner(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	for round := 0; round < 30; round++ {
		ownerID, _ := env.seedUser(t)
		memberID, member := env.seedUser(t)
		org := env.createOrg(t, ownerID)
		if err := env.orgs.AddMember(ctx, org.ID, ownerID, memberID, model.OrgRoleMember); err != nil {
			t.Fatalf("AddMember: %v", err)
		}
		race(time.Millisecond,
			func() { _ = env.users().DeleteUser(ctx, memberID) },
			func() { _ = env.orgs.TransferOrg(ctx, org.ID, ownerID, member) },
		)
		var owners int
		if err := env.db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND role = 'owner'`, org.ID).Scan(&owners); err != nil {
			t.Fatalf("count owners: %v", err)
		}
		if owners == 0 {
			t.Fatalf("round %d: the org was left without an owner", round)
		}
	}
}

// Owner-count changes serialize on the org row, so no interleaving of two
// owners leaving, demoting each other or deleting their accounts leaves none.
func TestOrgService_ConcurrentOwnerExits_KeepAnOwner(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	exits := map[string]func(org *model.Organization, id, other int64) error{
		"leave": func(org *model.Organization, id, _ int64) error {
			return env.orgs.RemoveMember(ctx, org.ID, id, id)
		},
		"demote other": func(org *model.Organization, id, other int64) error {
			return env.orgs.UpdateMemberRole(ctx, org.ID, id, other, model.OrgRoleMember)
		},
		"delete account": func(_ *model.Organization, id, _ int64) error {
			return env.users().DeleteUser(ctx, id)
		},
	}
	for round := 0; round < 3; round++ {
		for nameA, exitA := range exits {
			for nameB, exitB := range exits {
				aID, _ := env.seedUser(t)
				bID, _ := env.seedUser(t)
				org := env.coOwnedOrg(t, aID, bID)
				errs := make([]error, 2)
				start := make(chan struct{})
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); <-start; errs[0] = exitA(org, aID, bID) }()
				go func() { defer wg.Done(); <-start; errs[1] = exitB(org, bID, aID) }()
				close(start)
				wg.Wait()

				for _, err := range errs {
					if err != nil && strings.Contains(err.Error(), "deadlock") {
						t.Errorf("%s + %s: %v", nameA, nameB, err)
					}
				}
				var owners int
				if err := env.db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND role = 'owner'`, org.ID).Scan(&owners); err != nil {
					t.Fatalf("count owners: %v", err)
				}
				if owners == 0 {
					t.Fatalf("%s + %s at once left the org without an owner (errors %v)", nameA, nameB, errs)
				}
			}
		}
	}
}
