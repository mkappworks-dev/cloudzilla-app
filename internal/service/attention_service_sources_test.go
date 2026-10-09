package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type attentionEnv struct {
	db         *sql.DB
	viewerID   int64
	authorID   int64
	authorName string
	repoID     int64
	repoFull   string
	issues     *store.IssueStore
	pulls      *store.PullStore
}

func newAttentionEnv(t *testing.T) *attentionEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix+"a")
	viewerID := testutil.SeedUser(t, db, suffix+"v")
	authorName := "testuser_" + suffix + "a"
	repoID := testutil.SeedRepo(t, db, authorID, authorName, suffix)
	return &attentionEnv{
		db: db, viewerID: viewerID, authorID: authorID, authorName: authorName, repoID: repoID,
		repoFull: authorName + "/testrepo_" + suffix,
		issues:   store.NewIssueStore(db), pulls: store.NewPullStore(db),
	}
}

func (e *attentionEnv) issue(t *testing.T, authorID int64, title string) *model.Issue {
	t.Helper()
	i := &model.Issue{RepoID: e.repoID, AuthorID: authorID, Title: title, State: model.IssueStateOpen, Visibility: "public"}
	if err := e.issues.Create(context.Background(), i); err != nil {
		t.Fatalf("issue: %v", err)
	}
	return i
}

func (e *attentionEnv) pull(t *testing.T, authorID int64, title string, state model.PRState) *model.PullRequest {
	t.Helper()
	p := &model.PullRequest{RepoID: e.repoID, AuthorID: authorID, Title: title, State: state, HeadBranch: "h-" + title, BaseBranch: "main"}
	if err := e.pulls.Create(context.Background(), p); err != nil {
		t.Fatalf("pull: %v", err)
	}
	return p
}

func (e *attentionEnv) assignIssue(t *testing.T, issueID int64, at time.Time) {
	t.Helper()
	testutil.Exec(t, e.db, `INSERT INTO issue_assignees (issue_id, user_id, created_at) VALUES ($1, $2, $3)`, issueID, e.viewerID, at)
}

func (e *attentionEnv) assignPull(t *testing.T, pullID int64, at time.Time) {
	t.Helper()
	testutil.Exec(t, e.db, `INSERT INTO pull_assignees (pull_id, user_id, created_at) VALUES ($1, $2, $3)`, pullID, e.viewerID, at)
}

func (e *attentionEnv) requestReview(t *testing.T, pullID int64, at time.Time) {
	t.Helper()
	testutil.Exec(t, e.db,
		`INSERT INTO pull_reviews (pull_id, repo_id, author_id, state, created_at) VALUES ($1, $2, $3, 'pending', $4)`,
		pullID, e.repoID, e.viewerID, at)
}

func (e *attentionEnv) mention(t *testing.T, issueID, pullID int64, at time.Time) {
	t.Helper()
	var commentID int64
	var err error
	if issueID != 0 {
		err = e.db.QueryRow(`INSERT INTO comments (repo_id, issue_id, author_id, body, created_at) VALUES ($1, $2, $3, 'cc', $4) RETURNING id`,
			e.repoID, issueID, e.authorID, at).Scan(&commentID)
	} else {
		err = e.db.QueryRow(`INSERT INTO comments (repo_id, pull_id, author_id, body, created_at) VALUES ($1, $2, $3, 'cc', $4) RETURNING id`,
			e.repoID, pullID, e.authorID, at).Scan(&commentID)
	}
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	testutil.Exec(t, e.db, `INSERT INTO mentions (comment_id, user_id) VALUES ($1, $2)`, commentID, e.viewerID)
}

func (e *attentionEnv) service() *service.AttentionService {
	return service.NewAttentionService(e.issues).
		WithPullDeps(e.pulls, store.NewPullReviewStore(e.db), store.NewMentionStore(e.db)).
		WithUserStore(store.NewUserStore(e.db))
}

func TestAttentionService_ForUser_AllSourcesDedupedAndOrdered(t *testing.T) {
	e := newAttentionEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }

	assignedIssue := e.issue(t, e.authorID, "assigned issue")
	e.assignIssue(t, assignedIssue.ID, ago(10))
	e.mention(t, assignedIssue.ID, 0, ago(50))

	mentionedIssue := e.issue(t, e.authorID, "mentioned issue")
	e.mention(t, mentionedIssue.ID, 0, ago(30))

	own := e.issue(t, e.viewerID, "own issue")
	e.assignIssue(t, own.ID, ago(90))

	assignedPull := e.pull(t, e.authorID, "assigned-pr", model.PRStateOpen)
	e.assignPull(t, assignedPull.ID, ago(20))
	e.requestReview(t, assignedPull.ID, ago(60))

	reviewPull := e.pull(t, e.authorID, "review-pr", model.PRStateOpen)
	e.requestReview(t, reviewPull.ID, ago(40))

	mentionedPull := e.pull(t, e.authorID, "mentioned-pr", model.PRStateOpen)
	e.mention(t, 0, mentionedPull.ID, ago(5))

	closedPull := e.pull(t, e.authorID, "closed-pr", model.PRStateClosed)
	e.assignPull(t, closedPull.ID, ago(100))

	svc := e.service()
	items, err := svc.ForUser(ctx, e.viewerID)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		kind  service.AttentionKind
		url   string
		since time.Time
	}{
		{service.AttentionPRReviewRequested, fmt.Sprintf("/%s/pulls/%d", e.repoFull, reviewPull.Number), ago(40)},
		{service.AttentionIssueAssigned, fmt.Sprintf("/%s/issues/%d", e.repoFull, assignedIssue.Number), ago(10)},
		{service.AttentionPRAssigned, fmt.Sprintf("/%s/pulls/%d", e.repoFull, assignedPull.Number), ago(20)},
		{service.AttentionMention, fmt.Sprintf("/%s/issues/%d", e.repoFull, mentionedIssue.Number), ago(30)},
		{service.AttentionMention, fmt.Sprintf("/%s/pulls/%d", e.repoFull, mentionedPull.Number), ago(5)},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(items), len(want), items)
	}
	for i := 1; i < len(items); i++ {
		if items[i].WaitingSince.Before(items[i-1].WaitingSince) {
			t.Errorf("items not ordered oldest-waiting first: %v before %v", items[i-1].WaitingSince, items[i].WaitingSince)
		}
	}
	got := map[string]service.AttentionItem{}
	for _, it := range items {
		got[it.URL] = it
		if it.Actor != e.authorName {
			t.Errorf("item %q actor = %q, want %q", it.Title, it.Actor, e.authorName)
		}
	}
	for _, w := range want {
		it, ok := got[w.url]
		if !ok {
			t.Errorf("missing item url %s", w.url)
			continue
		}
		if it.Kind != w.kind || it.URL != w.url || !it.WaitingSince.Equal(w.since) || it.RepoName != e.repoFull {
			t.Errorf("item %q = kind %s url %s since %v repo %s; want kind %s url %s since %v",
				it.Title, it.Kind, it.URL, it.WaitingSince, it.RepoName, w.kind, w.url, w.since)
		}
	}

	// Cross-kind collisions: an assigned PR that also has a pending review must surface once, as assigned.
	if k := got[fmt.Sprintf("/%s/pulls/%d", e.repoFull, assignedPull.Number)].Kind; k != service.AttentionPRAssigned {
		t.Errorf("assigned+review PR kind = %s, want first match pr_assigned", k)
	}

	n, err := svc.CountForUser(ctx, e.viewerID)
	if err != nil || n != len(items) {
		t.Errorf("CountForUser = %d, %v; want %d", n, err, len(items))
	}
}

func TestAttentionService_ForUser_WithoutUserStoreLeavesActorEmpty(t *testing.T) {
	e := newAttentionEnv(t)
	i := e.issue(t, e.authorID, "assigned")
	e.assignIssue(t, i.ID, time.Now())

	svc := service.NewAttentionService(e.issues).
		WithPullDeps(e.pulls, store.NewPullReviewStore(e.db), store.NewMentionStore(e.db))
	items, err := svc.ForUser(context.Background(), e.viewerID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %v, err = %v", items, err)
	}
	if items[0].Actor != "" {
		t.Errorf("actor = %q, want empty without a user store", items[0].Actor)
	}
}

func TestAttentionService_ForUser_IssueOnlyMode(t *testing.T) {
	e := newAttentionEnv(t)
	now := time.Now().UTC()

	var ids []int64
	for i := 0; i < 22; i++ {
		is := e.issue(t, e.authorID, fmt.Sprintf("issue %02d", i))
		ids = append(ids, is.ID)
		e.assignIssue(t, is.ID, now.Add(-time.Duration(i)*time.Minute))
	}
	pr := e.pull(t, e.authorID, "ignored", model.PRStateOpen)
	e.assignPull(t, pr.ID, now)

	svc := service.NewAttentionService(e.issues).WithUserStore(store.NewUserStore(e.db))
	items, err := svc.ForUser(context.Background(), e.viewerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("got %d items, want 20 (capped)", len(items))
	}
	for _, it := range items {
		if it.Kind != service.AttentionIssueAssigned {
			t.Errorf("kind = %s; pull sources must be skipped without pull deps", it.Kind)
		}
		if it.Actor != e.authorName {
			t.Errorf("actor = %q", it.Actor)
		}
	}
	if items[0].RefID != ids[21] {
		t.Errorf("first item = %d, want the longest-waiting issue %d", items[0].RefID, ids[21])
	}
}

func TestAttentionService_ForUser_NothingPending(t *testing.T) {
	e := newAttentionEnv(t)
	svc := e.service()
	items, err := svc.ForUser(context.Background(), e.viewerID)
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %v, err = %v; want none", items, err)
	}
	if n, err := svc.CountForUser(context.Background(), e.viewerID); err != nil || n != 0 {
		t.Errorf("CountForUser = %d, %v", n, err)
	}
}

func TestAttentionService_ForUser_HidesItemsInUnreadablePrivateRepos(t *testing.T) {
	e := newAttentionEnv(t)
	i := e.issue(t, e.authorID, "now private")
	e.assignIssue(t, i.ID, time.Now())
	pr := e.pull(t, e.authorID, "private-pr", model.PRStateOpen)
	e.requestReview(t, pr.ID, time.Now())
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.repoID)

	items, err := e.service().ForUser(context.Background(), e.viewerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("items = %+v, want none for a repo the viewer cannot read", items)
	}
}
