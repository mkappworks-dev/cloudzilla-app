package service_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type closerFixture struct {
	db       *sql.DB
	svcs     *service.Services
	actor    service.CloseActor
	repo     *model.Repository
	pull     *model.PullRequest
	outsider int64
}

func newCloserFixture(t *testing.T) closerFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	svcs := service.New(store.New(db), &config.Config{Git: config.GitConfig{ReposRoot: t.TempDir()}})
	sfx := testutil.UniqueSuffix(t)
	actorID := testutil.SeedUser(t, db, sfx)
	f := closerFixture{db: db, svcs: svcs, actor: service.CloseActor{UserID: actorID, Username: "testuser_" + sfx}}
	f.outsider = testutil.SeedUser(t, db, sfx+"o")
	repoID := testutil.SeedRepo(t, db, actorID, f.actor.Username, sfx)
	repo, err := store.NewRepoStore(db).GetByID(context.Background(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	f.repo = repo
	f.pull = &model.PullRequest{RepoID: repoID, AuthorID: actorID, Title: "p", HeadBranch: "topic", BaseBranch: repo.DefaultBranch, State: model.PRStateOpen}
	if err := store.NewPullStore(db).Create(context.Background(), f.pull); err != nil {
		t.Fatal(err)
	}
	return f
}

// otherRepo seeds a repo owned by the outsider, with the actor as role when
// role isn't empty.
func (f closerFixture) otherRepo(t *testing.T, role string) int64 {
	t.Helper()
	sfx := testutil.UniqueSuffix(t)
	var ownerName string
	if err := f.db.QueryRow(`SELECT username FROM users WHERE id = $1`, f.outsider).Scan(&ownerName); err != nil {
		t.Fatal(err)
	}
	id := testutil.SeedRepo(t, f.db, f.outsider, ownerName, sfx)
	if role != "" {
		testutil.Exec(t, f.db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, $3)`, id, f.actor.UserID, role)
	}
	return id
}

// linkedIssue seeds an open issue by author in repoID and links it to the pull.
func (f closerFixture) linkedIssue(t *testing.T, repoID, author int64) int64 {
	t.Helper()
	var id int64
	if err := f.db.QueryRow(
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = $1), $2, 'i', '', 'open') RETURNING id`,
		repoID, author,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := store.NewIssueStore(f.db).LinkToPull(context.Background(), f.pull.ID, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f closerFixture) state(t *testing.T, issueID int64) string {
	t.Helper()
	var s string
	if err := f.db.QueryRow(`SELECT state FROM issues WHERE id = $1`, issueID).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (f closerFixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIssueCloser_ClosesOnlyWhereTheActorMayWrite(t *testing.T) {
	f := newCloserFixture(t)
	own := f.linkedIssue(t, f.repo.ID, f.outsider)
	writable := f.linkedIssue(t, f.otherRepo(t, "writer"), f.outsider)
	readOnly := f.linkedIssue(t, f.otherRepo(t, "reader"), f.outsider)
	noAccess := f.linkedIssue(t, f.otherRepo(t, ""), f.outsider)
	archivedRepo := f.otherRepo(t, "admin")
	testutil.Exec(t, f.db, `UPDATE repositories SET is_archived = true WHERE id = $1`, archivedRepo)
	archived := f.linkedIssue(t, archivedRepo, f.outsider)

	f.svcs.IssueCloser.CloseForPull(context.Background(), f.actor, f.repo, f.pull, nil)

	for name, tc := range map[string]struct {
		id   int64
		want string
	}{
		"own repo": {own, "closed"}, "writer elsewhere": {writable, "closed"},
		"reader elsewhere": {readOnly, "open"}, "no access": {noAccess, "open"}, "archived": {archived, "open"},
	} {
		if got := f.state(t, tc.id); got != tc.want {
			t.Errorf("%s: issue %s, want %s", name, got, tc.want)
		}
	}
}

func TestIssueCloser_TokenTargetsLimitTheRepos(t *testing.T) {
	f := newCloserFixture(t)
	own := f.linkedIssue(t, f.repo.ID, f.outsider)
	outside := f.linkedIssue(t, f.otherRepo(t, "writer"), f.outsider)
	f.actor.Targets = []string{f.repo.OwnerName + "/" + f.repo.Name}

	f.svcs.IssueCloser.CloseForPull(context.Background(), f.actor, f.repo, f.pull, nil)

	if got := f.state(t, own); got != "closed" {
		t.Errorf("issue in a target repo = %s, want closed", got)
	}
	if got := f.state(t, outside); got != "open" {
		t.Errorf("issue outside the targets = %s, want open", got)
	}
}

func TestIssueCloser_SideEffectsOnlyForIssuesItCloses(t *testing.T) {
	f := newCloserFixture(t)
	reported := f.linkedIssue(t, f.repo.ID, f.outsider)
	selfReported := f.linkedIssue(t, f.repo.ID, f.actor.UserID)
	alreadyClosed := f.linkedIssue(t, f.repo.ID, f.outsider)
	testutil.Exec(t, f.db, `UPDATE issues SET state = 'closed', closed_at = NOW() WHERE id = $1`, alreadyClosed)

	f.svcs.IssueCloser.CloseForPull(context.Background(), f.actor, f.repo, f.pull, nil)

	if n := f.count(t, `SELECT COUNT(*) FROM issue_events WHERE issue_id = ANY($1)`, []int64{reported, selfReported}); n != 2 {
		t.Errorf("closed events = %d, want 2", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM issue_events WHERE issue_id = $1`, alreadyClosed); n != 0 {
		t.Errorf("an already-closed issue got %d events", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'issue_closed' AND repo_id = $2`, f.outsider, f.repo.ID); n != 1 {
		t.Errorf("reporter notifications = %d, want 1 (not for the already-closed issue)", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'issue_closed'`, f.actor.UserID); n != 0 {
		t.Errorf("the actor was notified of closing their own issue %d times", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM events WHERE repo_id = $1 AND event_type = 'issue_closed'`, f.repo.ID); n != 2 {
		t.Errorf("issue_closed activity events = %d, want 2", n)
	}

	// The same pull merging again must not re-close a reopened issue.
	testutil.Exec(t, f.db, `UPDATE issues SET state = 'open', closed_at = NULL WHERE id = $1`, reported)
	f.svcs.IssueCloser.CloseForPull(context.Background(), f.actor, f.repo, f.pull, nil)
	if got := f.state(t, reported); got != "open" {
		t.Errorf("reopened issue = %s after the same pull closed it again, want open", got)
	}
}

func TestIssueCloser_ResolvesCommitReferences(t *testing.T) {
	f := newCloserFixture(t)
	privateRepo := f.otherRepo(t, "")
	testutil.Exec(t, f.db, `UPDATE repositories SET private = true WHERE id = $1`, privateRepo)
	var ownerName, privName string
	if err := f.db.QueryRow(`SELECT owner_name, name FROM repositories WHERE id = $1`, privateRepo).Scan(&ownerName, &privName); err != nil {
		t.Fatal(err)
	}
	var bare, hidden int64
	for _, q := range []struct {
		repo int64
		id   *int64
	}{{f.repo.ID, &bare}, {privateRepo, &hidden}} {
		if err := f.db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title, body, state) VALUES ($1, 5, $2, 'i', '', 'open') RETURNING id`, q.repo, f.outsider).Scan(q.id); err != nil {
			t.Fatal(err)
		}
	}
	refs := service.ParseClosingRefs("fixes #5, fixes " + ownerName + "/" + privName + "#5, fixes " + f.repo.OwnerName + "/" + f.repo.Name + "#5")

	f.svcs.IssueCloser.CloseForPull(context.Background(), f.actor, f.repo, f.pull, refs)

	if got := f.state(t, bare); got != "closed" {
		t.Errorf("#5 = %s, want closed", got)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM issue_events WHERE issue_id = $1`, bare); n != 1 {
		t.Errorf("#5 and its long form made %d events, want 1", n)
	}
	if got := f.state(t, hidden); got != "open" {
		t.Errorf("issue in a repo the actor can't read = %s, want open", got)
	}
}
