package service_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e repoDirsEnv) wantAlias(t *testing.T, owner, name string, repoID int64) {
	t.Helper()
	got, err := e.repos.ResolveAlias(context.Background(), owner, name)
	if err != nil || got.ID != repoID {
		t.Errorf("ResolveAlias(%s/%s) = %v, %v; want repo %d", owner, name, got, err, repoID)
	}
}

func TestRepoService_TransferRepo_ToAnOwnedOrgAliasesTheOldPath(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	repo := env.renamed(t, ownerID, owner, "src")
	testutil.Exec(t, env.db, `INSERT INTO events (actor_id, actor_name, repo_id, repo_name, owner_name, event_type, payload)
		VALUES ($1, 'a', $2, 'src', $3, 'issue_opened', '{}')`, ownerID, repo.ID, owner)
	testutil.Exec(t, env.db, `INSERT INTO notifications (user_id, actor_id, type, repo_id, repo_name, owner_name, subject_id, subject_url)
		VALUES ($1, $1, 'pr_opened', $2, 'src', $3, 1, '/`+owner+`/src/pulls/1')`, ownerID, repo.ID, owner)

	if transfer, err := env.repos.TransferRepo(ctx, repo, ownerID, org.Name); err != nil || transfer != nil {
		t.Fatalf("TransferRepo = %+v, %v; want an immediate move", transfer, err)
	}

	env.wantAlias(t, owner, "src", repo.ID)
	wantNoAlias(t, env, org.Name, "src")
	var eventOwner, noteOwner, subjectURL string
	if err := env.db.QueryRow(`SELECT owner_name FROM events WHERE repo_id = $1`, repo.ID).Scan(&eventOwner); err != nil || eventOwner != org.Name {
		t.Errorf("event owner = %q, %v; want %s", eventOwner, err, org.Name)
	}
	if err := env.db.QueryRow(`SELECT owner_name, subject_url FROM notifications WHERE repo_id = $1`, repo.ID).Scan(&noteOwner, &subjectURL); err != nil {
		t.Fatal(err)
	}
	if noteOwner != org.Name || subjectURL != "/"+org.Name+"/src/pulls/1" {
		t.Errorf("notification = %q / %q", noteOwner, subjectURL)
	}
}

func TestRepoService_AcceptTransfer_AliasesTheOldPathOnlyOnceAccepted(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	repo := env.renamed(t, fromID, from, "gift")
	transfer := env.offer(t, repo, fromID, to)

	wantNoAlias(t, env, from, "gift")

	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, transfer.FullName()); err != nil {
		t.Fatal(err)
	}
	env.wantAlias(t, from, "gift", repo.ID)
}

func TestRepoService_Transfer_FailedUpdateRestoresTheDirsAndLeavesNoAlias(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	org := env.createOrg(t, fromID)
	direct := env.renamed(t, fromID, from, "direct")
	offered := env.renamed(t, fromID, from, "offered")
	transfer := env.offer(t, offered, fromID, to)
	suffix := strings.ToLower(testutil.UniqueSuffix(t))
	testutil.Exec(t, env.db, `CREATE FUNCTION fail_move_`+suffix+`() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'injected'; END $$ LANGUAGE plpgsql`)
	testutil.Exec(t, env.db, `CREATE TRIGGER fail_move_`+suffix+` BEFORE UPDATE ON repositories
		FOR EACH ROW WHEN (NEW.id IN (`+strconv.FormatInt(direct.ID, 10)+`, `+strconv.FormatInt(offered.ID, 10)+`)
		                   AND NEW.owner_name <> OLD.owner_name) EXECUTE FUNCTION fail_move_`+suffix+`()`)
	t.Cleanup(func() {
		testutil.Exec(t, env.db, `DROP TRIGGER fail_move_`+suffix+` ON repositories`)
		testutil.Exec(t, env.db, `DROP FUNCTION fail_move_`+suffix+`()`)
	})

	if _, err := env.repos.TransferRepo(ctx, direct, fromID, org.Name); err == nil {
		t.Fatal("TransferRepo succeeded despite the failing update")
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, transfer.FullName()); err == nil {
		t.Fatal("AcceptTransfer succeeded despite the failing update")
	}

	for _, name := range []string{"direct", "offered"} {
		gitDir, wikiDir := env.dirs(from, name)
		if !pathExists(gitDir) || !pathExists(wikiDir) {
			t.Errorf("%s: dirs were not restored", name)
		}
		wantNoAlias(t, env, from, name)
	}
	if _, err := env.repos.PendingTransfer(ctx, offered.ID); err != nil {
		t.Errorf("the pending transfer was lost with the failed accept: %v", err)
	}
}

func TestRepoService_Transfer_BackToThePreviousOwnerLeavesNoSelfAlias(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	repo := env.renamed(t, ownerID, owner, "boomerang")

	if _, err := env.repos.TransferRepo(ctx, repo, ownerID, org.Name); err != nil {
		t.Fatal(err)
	}
	moved, err := env.repos.GetByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.repos.TransferRepo(ctx, moved, ownerID, owner); err != nil {
		t.Fatal(err)
	}

	wantNoAlias(t, env, owner, "boomerang")
	env.wantAlias(t, org.Name, "boomerang", repo.ID)
	if got, err := env.repos.Get(ctx, owner, "boomerang"); err != nil || got.ID != repo.ID {
		t.Errorf("Get(%s/boomerang) = %v, %v", owner, got, err)
	}
}

func TestRepoService_AcceptTransfer_BackToThePreviousOwnerLeavesNoSelfAlias(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	aID, a := env.seedUser(t)
	bID, b := env.seedUser(t)
	repo := env.renamed(t, aID, a, "pingpong")

	if err := env.transferTo(t, repo, aID, b); err != nil {
		t.Fatal(err)
	}
	var moved *model.Repository
	moved, err := env.repos.GetByID(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.transferTo(t, moved, bID, a); err != nil {
		t.Fatal(err)
	}

	wantNoAlias(t, env, a, "pingpong")
	env.wantAlias(t, b, "pingpong", repo.ID)
	env.wantUnder(t, repo.ID, a)
}
