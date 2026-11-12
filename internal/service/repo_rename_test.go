package service_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newRenameEnv(t *testing.T) repoDirsEnv {
	t.Helper()
	env, _ := newAliasEnv(t)
	env.repos.WithMirrorStore(store.NewMirrorStore(env.db))
	return env
}

func (e repoDirsEnv) renamed(t *testing.T, _ int64, owner, name string) *model.Repository {
	t.Helper()
	id := e.createWithWiki(t, owner, name)
	repo, err := e.repos.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestRepoService_Rename_MovesBothDirsAndTheRowAndAliasesTheOldPath(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "old")

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "fresh"); err != nil {
		t.Fatalf("RenameRepo: %v", err)
	}

	oldGit, oldWiki := env.dirs(owner, "old")
	newGit, newWiki := env.dirs(owner, "fresh")
	if pathExists(oldGit) || pathExists(oldWiki) {
		t.Error("old dirs are still on disk")
	}
	if !pathExists(newGit) || !pathExists(newWiki) {
		t.Error("new dirs are missing")
	}
	got, err := env.repos.Get(ctx, owner, "fresh")
	if err != nil || got.ID != repo.ID {
		t.Fatalf("Get(fresh) = %v, %v; want repo %d", got, err, repo.ID)
	}
	if alias, err := env.repos.ResolveAlias(ctx, owner, "old"); err != nil || alias.ID != repo.ID {
		t.Errorf("ResolveAlias(old) = %v, %v; want repo %d", alias, err, repo.ID)
	}

	again, err := env.repos.Get(ctx, owner, "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.repos.RenameRepo(ctx, again, ownerID, "old"); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	wantNoAlias(t, env, owner, "old")
	if alias, err := env.repos.ResolveAlias(ctx, owner, "fresh"); err != nil || alias.ID != repo.ID {
		t.Errorf("ResolveAlias(fresh) = %v, %v; want repo %d", alias, err, repo.ID)
	}
}

func TestRepoService_Rename_ChainStaysFlat(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "a")
	for _, next := range []string{"b", "c"} {
		cur, err := env.repos.GetByID(ctx, repo.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := env.repos.RenameRepo(ctx, cur, ownerID, next); err != nil {
			t.Fatalf("rename to %s: %v", next, err)
		}
	}
	for _, old := range []string{"a", "b"} {
		if got, err := env.repos.ResolveAlias(ctx, owner, old); err != nil || got.ID != repo.ID || got.Name != "c" {
			t.Errorf("ResolveAlias(%s) = %v, %v; want repo %d named c", old, got, err, repo.ID)
		}
	}
}

func TestRepoService_Rename_CaseOnly(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "lower")

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "Lower"); err != nil {
		t.Fatalf("case-only rename: %v", err)
	}

	got, err := env.repos.GetByID(ctx, repo.ID)
	if err != nil || got.Name != "Lower" {
		t.Fatalf("name = %v, %v; want Lower", got, err)
	}
	gitDir, wikiDir := env.dirs(owner, "Lower")
	if !pathExists(gitDir) || !pathExists(wikiDir) {
		t.Error("dirs are missing under the new case")
	}
	wantNoAlias(t, env, owner, "lower")
}

func TestRepoService_Rename_Refusals(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "src")
	env.renamed(t, ownerID, owner, "taken")
	if _, err := env.repos.Create(ctx, ownerID, owner, "wikiholder", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}
	mirrored := env.renamed(t, ownerID, owner, "mirrored")
	testutil.Exec(t, env.db, `INSERT INTO repo_mirrors (repo_id, remote_url, interval_seconds, next_sync_at, created_by, lease_until)
		VALUES ($1, 'https://example.test/x.git', 3600, NOW(), $2, NOW() + interval '5 minutes')`, mirrored.ID, ownerID)

	tests := []struct {
		name string
		repo *model.Repository
		to   string
		want error
	}{
		{"taken", repo, "taken", service.ErrRepoNameTaken},
		{"taken ignoring case", repo, "TAKEN", service.ErrRepoNameTaken},
		{"wiki name", repo, "foo.wiki", service.ErrRepoNameReserved},
		{"invalid", repo, "bad name", service.ErrInvalidRepoName},
		{"empty", repo, "", service.ErrInvalidRepoName},
		{"unchanged", repo, "src", service.ErrRepoNameUnchanged},
		{"mirror syncing", mirrored, "elsewhere", service.ErrRepoSyncing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := env.repos.RenameRepo(ctx, tt.repo, ownerID, tt.to); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
	got, err := env.repos.GetByID(ctx, repo.ID)
	if err != nil || got.Name != "src" {
		t.Errorf("refused renames changed the repo: %v, %v", got, err)
	}
}

func TestRepoService_Rename_ATakenDirWithoutARowIsRefused(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "src")
	env.createWithWiki(t, owner, "ghost")
	env.dropRow(t, owner, "ghost")

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "ghost"); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("err = %v, want ErrRepoNameTaken", err)
	}
	if gitDir, wikiDir := env.dirs(owner, "src"); !pathExists(gitDir) || !pathExists(wikiDir) {
		t.Error("a refused rename moved the dirs")
	}
}

func TestRepoService_Rename_ADroppedAliasMakesWayForTheNewName(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	other := env.renamed(t, ownerID, owner, "other")
	if err := env.repos.RenameRepo(ctx, other, ownerID, "other2"); err != nil {
		t.Fatal(err)
	}
	repo := env.renamed(t, ownerID, owner, "src")

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "other"); err != nil {
		t.Fatalf("rename onto another repo's alias: %v", err)
	}
	wantNoAlias(t, env, owner, "other")
	if got, err := env.repos.Get(ctx, owner, "other"); err != nil || got.ID != repo.ID {
		t.Errorf("Get(other) = %v, %v; want repo %d", got, err, repo.ID)
	}
}

func TestRepoService_Rename_FailedTransactionRestoresTheDirs(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "src")
	suffix := strings.ToLower(testutil.UniqueSuffix(t))
	testutil.Exec(t, env.db, `CREATE FUNCTION fail_rename_`+suffix+`() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'injected'; END $$ LANGUAGE plpgsql`)
	testutil.Exec(t, env.db, `CREATE TRIGGER fail_rename_`+suffix+` BEFORE UPDATE ON repositories
		FOR EACH ROW WHEN (NEW.id = `+strconv.FormatInt(repo.ID, 10)+` AND NEW.name = 'doomed') EXECUTE FUNCTION fail_rename_`+suffix+`()`)
	t.Cleanup(func() {
		testutil.Exec(t, env.db, `DROP TRIGGER fail_rename_`+suffix+` ON repositories`)
		testutil.Exec(t, env.db, `DROP FUNCTION fail_rename_`+suffix+`()`)
	})

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "doomed"); err == nil {
		t.Fatal("RenameRepo succeeded despite the failing update")
	}

	gitDir, wikiDir := env.dirs(owner, "src")
	if !pathExists(gitDir) || !pathExists(wikiDir) {
		t.Error("dirs were not restored")
	}
	doomedGit, doomedWiki := env.dirs(owner, "doomed")
	if pathExists(doomedGit) || pathExists(doomedWiki) {
		t.Error("dirs are left at the new path")
	}
	if got, err := env.repos.Get(ctx, owner, "src"); err != nil || got.ID != repo.ID {
		t.Errorf("Get(src) = %v, %v", got, err)
	}
	wantNoAlias(t, env, owner, "src")
}

func TestRepoService_Rename_Roles(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	adminID, admin := env.seedUser(t)
	writerID, writer := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "src")
	for username, role := range map[string]string{admin: "admin", writer: "writer"} {
		if err := env.repos.AddCollaborator(ctx, repo, ownerID, username, role); err != nil {
			t.Fatalf("add %s: %v", role, err)
		}
	}

	if err := env.repos.RenameRepo(ctx, repo, writerID, "byw"); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("writer rename: err = %v, want ErrForbidden", err)
	}
	if err := env.repos.RenameRepo(ctx, repo, adminID, "bya"); err != nil {
		t.Errorf("admin rename: %v", err)
	}
}

func TestRepoService_Rename_RelabelsEventsAndNotificationsButNotPayloads(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "old")
	other := env.renamed(t, ownerID, owner, "bystander")
	for _, r := range []*model.Repository{repo, other} {
		testutil.Exec(t, env.db, `INSERT INTO events (actor_id, actor_name, repo_id, repo_name, owner_name, event_type, payload)
			VALUES ($1, 'a', $2, $3, $4, 'issue_opened', '{"title":"see /`+owner+`/old"}')`, ownerID, r.ID, r.Name, owner)
		testutil.Exec(t, env.db, `INSERT INTO notifications (user_id, actor_id, type, repo_id, repo_name, owner_name, subject_id, subject_url)
			VALUES ($1, $1, 'pr_opened', $2, $3, $4, 1, '/`+owner+`/`+r.Name+`/pulls/1')`, ownerID, r.ID, r.Name, owner)
	}

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "fresh"); err != nil {
		t.Fatal(err)
	}

	var repoName, payload, subjectURL, noteRepo string
	if err := env.db.QueryRow(`SELECT repo_name, payload->>'title' FROM events WHERE repo_id = $1`, repo.ID).Scan(&repoName, &payload); err != nil {
		t.Fatal(err)
	}
	if repoName != "fresh" || payload != "see /"+owner+"/old" {
		t.Errorf("event = %q / %q; want name fresh and the payload as recorded", repoName, payload)
	}
	if err := env.db.QueryRow(`SELECT repo_name, subject_url FROM notifications WHERE repo_id = $1`, repo.ID).Scan(&noteRepo, &subjectURL); err != nil {
		t.Fatal(err)
	}
	if noteRepo != "fresh" || subjectURL != "/"+owner+"/fresh/pulls/1" {
		t.Errorf("notification = %q / %q", noteRepo, subjectURL)
	}
	if err := env.db.QueryRow(`SELECT repo_name, subject_url FROM notifications WHERE repo_id = $1`, other.ID).Scan(&noteRepo, &subjectURL); err != nil {
		t.Fatal(err)
	}
	if noteRepo != "bystander" || subjectURL != "/"+owner+"/bystander/pulls/1" {
		t.Errorf("another repo's notification changed: %q / %q", noteRepo, subjectURL)
	}
}

func TestRepoService_Rename_KeepsPullsMirrorWebhooksForksAndWiki(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "src")
	fork := env.renamed(t, ownerID, owner, "forked")
	testutil.Exec(t, env.db, `UPDATE repositories SET is_fork = TRUE, fork_of_id = $1 WHERE id = $2`, repo.ID, fork.ID)
	testutil.Exec(t, env.db, `INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 1, $2, 'open pr', 'feature')`, repo.ID, ownerID)
	testutil.Exec(t, env.db, `INSERT INTO webhooks (repo_id, url) VALUES ($1, 'https://example.test/hook')`, repo.ID)
	testutil.Exec(t, env.db, `INSERT INTO repo_mirrors (repo_id, remote_url, interval_seconds, next_sync_at, created_by)
		VALUES ($1, 'https://example.test/x.git', 3600, NOW(), $2)`, repo.ID, ownerID)

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "fresh"); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"pull_requests", "webhooks", "repo_mirrors"} {
		var n int
		if err := env.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE repo_id = $1`, repo.ID).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s rows for the repo = %d, %v; want 1", table, n, err)
		}
	}
	got, err := env.repos.Get(ctx, owner, "forked")
	if err != nil || got.ForkOfName != "fresh" {
		t.Errorf("fork label = %q, %v; want fresh", got.ForkOfName, err)
	}
	content, found, err := env.code.WikiPageGet(owner, "fresh", "Home")
	if err != nil || !found || !strings.Contains(content, "wiki of "+owner+"/src") {
		t.Errorf("wiki page = %q, %v, %v", content, found, err)
	}
}

func TestRepoService_AcceptTransfer_AfterARenameIsRefused(t *testing.T) {
	env := newRenameEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	_, to := env.seedUser(t)
	repo := env.renamed(t, ownerID, owner, "src")
	transfer := env.offer(t, repo, ownerID, to)

	if err := env.repos.RenameRepo(ctx, repo, ownerID, "fresh"); err != nil {
		t.Fatal(err)
	}

	_, err := env.repos.AcceptTransfer(ctx, transfer.ID, transfer.RecipientID, transfer.FullName())
	if !errors.Is(err, service.ErrTransferChanged) {
		t.Errorf("err = %v, want ErrTransferChanged", err)
	}
	if _, err := env.repos.PendingTransfer(ctx, repo.ID); err != nil {
		t.Errorf("the transfer was lost: %v", err)
	}
}
