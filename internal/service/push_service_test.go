package service_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type pushFixture struct {
	svcs   *service.Services
	repo   *model.Repository
	actor  service.CloseActor
	author service.GitAuthor
	db     *sql.DB
	hits   *atomic.Int32
	count  func(query string) int
}

func newPushFixture(t *testing.T) pushFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	svcs := service.New(store.New(db), &config.Config{
		Git:     config.GitConfig{ReposRoot: root},
		Webhook: config.WebhookConfig{AllowLocalNetworks: true},
	})
	sfx := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, sfx)
	owner := "testuser_" + sfx
	repoID := testutil.SeedRepo(t, db, userID, owner, sfx)
	repo, err := store.NewRepoStore(db).GetByID(context.Background(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gogit.PlainInit(filepath.Join(root, owner, repo.Name+".git"), true); err != nil {
		t.Fatal(err)
	}

	var hits atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Cloudzilla-Event") == "push" {
			hits.Add(1)
		}
	}))
	t.Cleanup(hook.Close)
	if _, err := svcs.Webhook.Create(context.Background(), repoID, hook.URL, "", "push"); err != nil {
		t.Fatal(err)
	}
	return pushFixture{
		svcs: svcs, repo: repo, db: db, hits: &hits,
		actor:  service.CloseActor{UserID: userID, Username: owner},
		author: service.GitAuthor{Name: owner, Email: owner + "@example.test"},
		count: func(q string) int {
			var n int
			if err := db.QueryRow(q, repoID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		},
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

const pushEvents = `SELECT count(*) FROM events WHERE repo_id = $1 AND event_type = 'push'`

func TestAfterWebCommit_RunsWhatAPushRuns(t *testing.T) {
	f := newPushFixture(t)
	upd, err := f.svcs.Code.CommitFile(f.repo.OwnerName, f.repo.Name, "main", "go.mod", []byte("module x\n"), f.author, "Add go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if upd.Branch != "main" || !upd.Old.IsZero() || upd.New.IsZero() {
		t.Fatalf("first commit of an empty repo = %+v, want a create on main", upd)
	}

	f.svcs.Push.AfterWebCommit(f.repo, f.actor, upd)

	eventually(t, "the push webhook", func() bool { return f.hits.Load() == 1 })
	eventually(t, "the push activity event", func() bool { return f.count(pushEvents) == 1 })
}

func TestAfterWebCommit_NoActorSkipsActivity(t *testing.T) {
	f := newPushFixture(t)
	upd, err := f.svcs.Code.CommitFile(f.repo.OwnerName, f.repo.Name, "main", "a.txt", []byte("a\n"), f.author, "Add a")
	if err != nil {
		t.Fatal(err)
	}

	f.svcs.Push.AfterWebCommit(f.repo, service.CloseActor{}, upd)

	eventually(t, "the push webhook", func() bool { return f.hits.Load() == 1 })
	time.Sleep(200 * time.Millisecond)
	if n := f.count(pushEvents); n != 0 {
		t.Errorf("a push with no human actor recorded %d activity events, want 0", n)
	}
}

func TestAfterWebCommit_UpdatesAnOpenPullRequestsHead(t *testing.T) {
	f := newPushFixture(t)
	if _, err := f.svcs.Code.CommitFile(f.repo.OwnerName, f.repo.Name, "main", "a.txt", []byte("a\n"), f.author, "Add a"); err != nil {
		t.Fatal(err)
	}
	pull := &model.PullRequest{RepoID: f.repo.ID, AuthorID: f.actor.UserID, Title: "p", HeadBranch: "topic", BaseBranch: "main", State: model.PRStateOpen}
	if err := store.NewPullStore(f.db).Create(context.Background(), pull); err != nil {
		t.Fatal(err)
	}
	upd, err := f.svcs.Code.CommitFile(f.repo.OwnerName, f.repo.Name, "topic", "b.txt", []byte("b\n"), f.author, "Add b")
	if err != nil {
		t.Fatal(err)
	}

	f.svcs.Push.AfterWebCommit(f.repo, f.actor, upd)

	eventually(t, "the pull request's head SHA", func() bool {
		var sha sql.NullString
		if err := f.db.QueryRow(`SELECT head_sha FROM pull_requests WHERE id = $1`, pull.ID).Scan(&sha); err != nil {
			t.Fatal(err)
		}
		return sha.String == upd.New.String()
	})
}

func TestAfterWebCommit_ClosesIssuesNamedByTheCommit(t *testing.T) {
	f := newPushFixture(t)
	if _, err := f.svcs.Code.CommitFile(f.repo.OwnerName, f.repo.Name, "main", "a.txt", []byte("a\n"), f.author, "Add a"); err != nil {
		t.Fatal(err)
	}
	var issueID int64
	if err := f.db.QueryRow(
		`INSERT INTO issues (repo_id, number, author_id, title, body, state) VALUES ($1, 1, $2, 'i', '', 'open') RETURNING id`,
		f.repo.ID, f.actor.UserID,
	).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	upd, err := f.svcs.Code.CommitFile(f.repo.OwnerName, f.repo.Name, "main", "b.txt", []byte("b\n"), f.author, "Fixes #1")
	if err != nil {
		t.Fatal(err)
	}

	f.svcs.Push.AfterWebCommit(f.repo, f.actor, upd)

	eventually(t, "the issue to close", func() bool {
		var state string
		if err := f.db.QueryRow(`SELECT state FROM issues WHERE id = $1`, issueID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return model.IssueState(state) == model.IssueStateClosed
	})
}
