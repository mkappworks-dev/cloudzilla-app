package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newImportEnv(t *testing.T, allowLocal bool) (*service.ImportService, *service.RepoService, *sql.DB, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	git := config.GitConfig{ReposRoot: t.TempDir()}
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, git)
	imports := service.NewImportService(repoSvc, git, config.ImportConfig{AllowLocalNetworks: allowLocal, Timeout: time.Minute})
	return imports, repoSvc, db, git.ReposRoot
}

func waitImport(t *testing.T, imports *service.ImportService, userID int64, id string) service.ImportJob {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		job, err := imports.Get(userID, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if job.Finished() {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("import %s still %s", id, job.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestImport_PublishesTheSource(t *testing.T) {
	imports, repoSvc, db, root := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	src := testutil.SeedSourceRepo(t)

	job, err := imports.Start(ctx, uid, uname, service.ImportRequest{
		CloneURL: testutil.ServeGitHTTP(t, src.Dir, "", ""), Name: "imported", Description: "copied", Private: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if job.Status != service.ImportQueued || job.Owner != uname || job.Name != "imported" {
		t.Errorf("started job = %+v", job)
	}
	if done := waitImport(t, imports, uid, job.ID); done.Status != service.ImportDone {
		t.Fatalf("status %s: %s", done.Status, done.Error)
	}
	repo, err := repoSvc.Get(ctx, uname, "imported")
	if err != nil || repo.DefaultBranch != "develop" || !repo.Private || repo.Description != "copied" {
		t.Errorf("repo = %+v, %v", repo, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".import-tmp", job.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp clone left behind: %v", err)
	}
}

func TestImport_PrivateSource(t *testing.T) {
	imports, _, db, _ := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "alice", "s3cret")

	anon, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: url, Name: "anon"})
	if err != nil {
		t.Fatalf("Start anon: %v", err)
	}
	got := waitImport(t, imports, uid, anon.ID)
	if got.Status != service.ImportFailed || got.Error != "Repository not found, or it needs a username and token." {
		t.Errorf("without credentials: %s %q", got.Status, got.Error)
	}

	authed, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: url, AuthUsername: "alice", AuthToken: "s3cret", Name: "authed"})
	if err != nil {
		t.Fatalf("Start authed: %v", err)
	}
	if got := waitImport(t, imports, uid, authed.ID); got.Status != service.ImportDone {
		t.Errorf("with credentials: %s %q", got.Status, got.Error)
	}
}

func TestImport_BlocksPrivateNetworksByDefault(t *testing.T) {
	imports, repoSvc, db, _ := newImportEnv(t, false)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", "")

	job, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: url, Name: "blocked"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := waitImport(t, imports, uid, job.ID)
	want := "127.0.0.1 resolves to a private network address. An administrator can allow this with import.allow_local_networks."
	if got.Status != service.ImportFailed || got.Error != want {
		t.Errorf("got %s %q, want failed %q", got.Status, got.Error, want)
	}
	if _, err := repoSvc.Get(ctx, uname, "blocked"); err == nil {
		t.Error("a refused import created a repository")
	}
}

func TestImport_RefusesATakenName(t *testing.T) {
	imports, repoSvc, db, _ := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	if _, err := repoSvc.Create(ctx, uid, uname, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: "https://example.com/a.git", Name: "taken"})
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("err = %v, want ErrRepoNameTaken", err)
	}
}

func TestImport_JobsArePrivateToTheirUser(t *testing.T) {
	imports, _, db, _ := newImportEnv(t, false)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	otherID, _ := seedImportUser(t, db)

	job, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: "http://127.0.0.1:1/x.git", Name: "mine"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitImport(t, imports, uid, job.ID)
	if _, err := imports.Get(otherID, job.ID); !errors.Is(err, service.ErrImportNotFound) {
		t.Errorf("other user's Get: err = %v, want ErrImportNotFound", err)
	}
}

func TestImport_StartEvictsOldFinishedJobs(t *testing.T) {
	imports, _, db, _ := newImportEnv(t, false)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)

	var ids []string
	for i := range 22 {
		job, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: "http://127.0.0.1:1/x.git", Name: fmt.Sprintf("refused%d", i)})
		if err != nil {
			t.Fatalf("import %d: %v", i, err)
		}
		waitImport(t, imports, uid, job.ID)
		ids = append(ids, job.ID)
	}
	if _, err := imports.Get(uid, ids[0]); !errors.Is(err, service.ErrImportNotFound) {
		t.Errorf("oldest finished job: err = %v, want ErrImportNotFound", err)
	}
	if _, err := imports.Get(uid, ids[1]); err != nil {
		t.Errorf("second oldest finished job: %v", err)
	}
}

func TestImport_LimitsActiveImportsPerUser(t *testing.T) {
	imports, _, db, _ := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	handler := testutil.GitHTTPHandler(t, testutil.SeedSourceRepo(t).Dir, "", "")
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release) // before srv.Close, which waits for the held requests

	var ids []string
	for i := range 5 {
		job, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: srv.URL + "/source.git", Name: fmt.Sprintf("held%d", i)})
		if err != nil {
			t.Fatalf("import %d: %v", i, err)
		}
		ids = append(ids, job.ID)
	}
	if _, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: srv.URL + "/source.git", Name: "sixth"}); !errors.Is(err, service.ErrTooManyImports) {
		t.Errorf("sixth import: err = %v, want ErrTooManyImports", err)
	}
	release()
	for _, id := range ids {
		waitImport(t, imports, uid, id)
	}
}
