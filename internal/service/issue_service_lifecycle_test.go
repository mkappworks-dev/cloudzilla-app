package service_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type issueLifeEnv struct {
	db       *sql.DB
	svc      *service.IssueService
	owner    string
	repoName string
	repoID   int64
	ownerID  int64
	writerID int64
	otherID  int64
	adminID  int64
}

func newIssueLifeEnv(t *testing.T) *issueLifeEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	writerID := testutil.SeedUser(t, db, suffix+"w")
	otherID := testutil.SeedUser(t, db, suffix+"o")
	adminID := testutil.SeedUser(t, db, suffix+"a")
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, repoID, writerID)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'admin')`, repoID, adminID)

	repoStore := store.NewRepoStore(db)
	repoSvc := service.NewRepoService(repoStore, store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{})
	svc := service.NewIssueService(store.NewIssueStore(db), repoStore, store.NewPullStore(db), repoSvc).
		WithEventStore(store.NewIssueEventStore(db)).
		WithMentionStore(store.NewMentionStore(db))
	return &issueLifeEnv{
		db: db, svc: svc, owner: ownerName, repoName: "testrepo_" + suffix, repoID: repoID,
		ownerID: ownerID, writerID: writerID, otherID: otherID, adminID: adminID,
	}
}

func (e *issueLifeEnv) issue(t *testing.T, title, visibility string) *model.Issue {
	t.Helper()
	i, err := e.svc.Create(context.Background(), e.owner, e.repoName, e.ownerID, title, "body", visibility)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return i
}

func issueLifePtr[T any](v T) *T { return &v }

func TestIssueService_Create_Guards(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()

	if _, err := e.svc.Create(ctx, e.owner, e.repoName, e.ownerID, strings.Repeat("x", service.MaxTitleLen+1), "", ""); !errors.Is(err, service.ErrTitleTooLong) {
		t.Errorf("long title: err = %v, want ErrTitleTooLong", err)
	}
	if _, err := e.svc.Create(ctx, e.owner, e.repoName, e.ownerID, strings.Repeat("x", service.MaxTitleLen), "", ""); err != nil {
		t.Errorf("title at limit: err = %v", err)
	}
	if _, err := e.svc.Create(ctx, e.owner, e.repoName, e.otherID, "t", "", "private"); !errors.Is(err, service.ErrPrivateIssueForbidden) {
		t.Errorf("private by non-writer: err = %v, want ErrPrivateIssueForbidden", err)
	}
	if _, err := e.svc.Create(ctx, e.owner, e.repoName, e.writerID, "t", "", "private"); err != nil {
		t.Errorf("private by writer: err = %v", err)
	}

	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.repoID)
	if _, err := e.svc.Create(ctx, e.owner, e.repoName, e.otherID, "t", "", ""); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("unreadable private repo: err = %v, want forbidden", err)
	}
}

func TestIssueService_UnknownRepo(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	const missing = "no_such_repo"

	calls := map[string]func() error{
		"List": func() error { _, err := e.svc.List(ctx, e.owner, missing, nil); return err },
		"Get":  func() error { _, err := e.svc.Get(ctx, e.owner, missing, 1, nil); return err },
		"SetState": func() error {
			_, err := e.svc.SetState(ctx, e.owner, missing, 1, model.IssueStateClosed, e.ownerID, "x")
			return err
		},
		"SetPriority": func() error { _, err := e.svc.SetPriority(ctx, e.owner, missing, 1, nil); return err },
		"EditTitle":   func() error { _, err := e.svc.EditTitle(ctx, e.owner, missing, 1, "t"); return err },
		"EditBody":    func() error { _, err := e.svc.EditBody(ctx, e.owner, missing, 1, "b"); return err },
		"PinIssue":    func() error { return e.svc.PinIssue(ctx, e.owner, missing, 1, e.ownerID) },
		"UnpinIssue":  func() error { return e.svc.UnpinIssue(ctx, e.owner, missing, 1, e.ownerID) },
		"LockIssue":   func() error { return e.svc.LockIssue(ctx, e.owner, missing, 1, e.ownerID) },
		"UnlockIssue": func() error { return e.svc.UnlockIssue(ctx, e.owner, missing, 1, e.ownerID) },
		"ListPinned":  func() error { _, err := e.svc.ListPinned(ctx, e.owner, missing, nil); return err },
		"LinkedPRs":   func() error { _, err := e.svc.LinkedPRs(ctx, e.owner, missing, 1, nil); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil || !strings.Contains(err.Error(), "repo not found") {
				t.Fatalf("err = %v, want repo not found", err)
			}
		})
	}
}

func TestIssueService_UnknownIssueNumber(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()

	calls := map[string]func() error{
		"SetState": func() error {
			_, err := e.svc.SetState(ctx, e.owner, e.repoName, 99, model.IssueStateClosed, e.ownerID, "x")
			return err
		},
		"SetPriority": func() error { _, err := e.svc.SetPriority(ctx, e.owner, e.repoName, 99, nil); return err },
		"EditTitle":   func() error { _, err := e.svc.EditTitle(ctx, e.owner, e.repoName, 99, "t"); return err },
		"EditBody":    func() error { _, err := e.svc.EditBody(ctx, e.owner, e.repoName, 99, "b"); return err },
		"Get":         func() error { _, err := e.svc.Get(ctx, e.owner, e.repoName, 99, nil); return err },
		"PinIssue":    func() error { return e.svc.PinIssue(ctx, e.owner, e.repoName, 99, e.ownerID) },
		"UnpinIssue":  func() error { return e.svc.UnpinIssue(ctx, e.owner, e.repoName, 99, e.ownerID) },
		"LockIssue":   func() error { return e.svc.LockIssue(ctx, e.owner, e.repoName, 99, e.ownerID) },
		"UnlockIssue": func() error { return e.svc.UnlockIssue(ctx, e.owner, e.repoName, 99, e.ownerID) },
		"LinkedPRs":   func() error { _, err := e.svc.LinkedPRs(ctx, e.owner, e.repoName, 99, nil); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("err = nil, want not found")
			}
		})
	}
}

func TestIssueService_Get_PrivateIssueVisibility(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	priv := e.issue(t, "secret", "private")

	if _, err := e.svc.Get(ctx, e.owner, e.repoName, priv.Number, nil); err == nil {
		t.Error("anonymous viewer must not see a private issue")
	}
	if _, err := e.svc.Get(ctx, e.owner, e.repoName, priv.Number, &e.otherID); err == nil {
		t.Error("stranger must not see a private issue")
	}
	got, err := e.svc.Get(ctx, e.owner, e.repoName, priv.Number, &e.ownerID)
	if err != nil || got.ID != priv.ID {
		t.Errorf("author view: issue = %v, err = %v", got, err)
	}

	list, err := e.svc.List(ctx, e.owner, e.repoName, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("anonymous List = %d issues, want 0", len(list))
	}
}

func TestIssueService_SetState_RecordsTimelineEvents(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	i := e.issue(t, "toggle", "")

	closed, err := e.svc.SetState(ctx, e.owner, e.repoName, i.Number, model.IssueStateClosed, e.ownerID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != model.IssueStateClosed || closed.ClosedAt == nil {
		t.Errorf("closed = %+v", closed)
	}
	if _, err := e.svc.SetState(ctx, e.owner, e.repoName, i.Number, model.IssueStateClosed, e.ownerID, "owner"); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
	reopened, err := e.svc.SetState(ctx, e.owner, e.repoName, i.Number, model.IssueStateOpen, e.ownerID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State != model.IssueStateOpen || reopened.ClosedAt != nil {
		t.Errorf("reopened = %+v", reopened)
	}

	events, err := e.svc.Events(ctx, i.ID, &e.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != model.IssueEventClosed || events[1].Type != model.IssueEventReopened {
		t.Errorf("events = %+v, want closed then reopened (no-op close must not log)", events)
	}
	if events[0].ActorName != "owner" || events[0].ActorID != e.ownerID {
		t.Errorf("actor = %d/%q", events[0].ActorID, events[0].ActorName)
	}
}

func TestIssueService_SetState_WithoutEventStore(t *testing.T) {
	e := newIssueLifeEnv(t)
	repoStore := store.NewRepoStore(e.db)
	repoSvc := service.NewRepoService(repoStore, store.NewUserStore(e.db), store.NewOrgStore(e.db), nil, nil, config.GitConfig{})
	bare := service.NewIssueService(store.NewIssueStore(e.db), repoStore, store.NewPullStore(e.db), repoSvc)
	i := e.issue(t, "plain", "")

	got, err := bare.SetState(context.Background(), e.owner, e.repoName, i.Number, model.IssueStateClosed, e.ownerID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.IssueStateClosed || got.ClosedAt == nil {
		t.Errorf("issue = %+v", got)
	}
}

func TestIssueService_Edits(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	i := e.issue(t, "orig", "")

	if _, err := e.svc.EditTitle(ctx, e.owner, e.repoName, i.Number, strings.Repeat("x", service.MaxTitleLen+1)); !errors.Is(err, service.ErrTitleTooLong) {
		t.Errorf("long title: err = %v", err)
	}
	got, err := e.svc.EditTitle(ctx, e.owner, e.repoName, i.Number, "renamed")
	if err != nil || got.Title != "renamed" {
		t.Errorf("EditTitle = %v, %v", got, err)
	}
	got, err = e.svc.EditBody(ctx, e.owner, e.repoName, i.Number, "new body")
	if err != nil || got.Body != "new body" {
		t.Errorf("EditBody = %v, %v", got, err)
	}

	got, err = e.svc.SetPriority(ctx, e.owner, e.repoName, i.Number, issueLifePtr("P1"))
	if err != nil || got.Priority == nil || *got.Priority != "P1" {
		t.Errorf("SetPriority P1 = %+v, %v", got, err)
	}
	got, err = e.svc.SetPriority(ctx, e.owner, e.repoName, i.Number, nil)
	if err != nil || got.Priority != nil {
		t.Errorf("SetPriority nil = %+v, %v", got, err)
	}
	if _, err := e.svc.SetPriority(ctx, e.owner, e.repoName, i.Number, issueLifePtr("P9")); err == nil {
		t.Error("an unknown priority must be rejected by the database check")
	}
}

func TestIssueService_PinLockPermissions(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	i := e.issue(t, "managed", "")

	actions := map[string]func(uid int64) error{
		"PinIssue":    func(uid int64) error { return e.svc.PinIssue(ctx, e.owner, e.repoName, i.Number, uid) },
		"UnpinIssue":  func(uid int64) error { return e.svc.UnpinIssue(ctx, e.owner, e.repoName, i.Number, uid) },
		"LockIssue":   func(uid int64) error { return e.svc.LockIssue(ctx, e.owner, e.repoName, i.Number, uid) },
		"UnlockIssue": func(uid int64) error { return e.svc.UnlockIssue(ctx, e.owner, e.repoName, i.Number, uid) },
	}
	for name, act := range actions {
		for who, uid := range map[string]int64{"stranger": e.otherID, "writer": e.writerID} {
			t.Run(name+"/"+who, func(t *testing.T) {
				if err := act(uid); err == nil || err.Error() != "forbidden" {
					t.Fatalf("err = %v, want forbidden", err)
				}
			})
		}
	}

	if err := e.svc.PinIssue(ctx, e.owner, e.repoName, i.Number, e.adminID); err != nil {
		t.Fatalf("admin pin: %v", err)
	}
	if err := e.svc.LockIssue(ctx, e.owner, e.repoName, i.Number, e.ownerID); err != nil {
		t.Fatalf("owner lock: %v", err)
	}
	got, err := e.svc.Get(ctx, e.owner, e.repoName, i.Number, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPinned || !got.IsLocked || got.LockedAt == nil {
		t.Errorf("issue = pinned %v locked %v lockedAt %v", got.IsPinned, got.IsLocked, got.LockedAt)
	}
	if err := e.svc.UnlockIssue(ctx, e.owner, e.repoName, i.Number, e.ownerID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.UnpinIssue(ctx, e.owner, e.repoName, i.Number, e.ownerID); err != nil {
		t.Fatal(err)
	}
	got, _ = e.svc.Get(ctx, e.owner, e.repoName, i.Number, nil)
	if got.IsPinned || got.IsLocked || got.LockedAt != nil {
		t.Errorf("after undo: pinned %v locked %v lockedAt %v", got.IsPinned, got.IsLocked, got.LockedAt)
	}
}

func TestIssueService_PinLimit(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()

	var issues []*model.Issue
	for _, title := range []string{"a", "b", "c", "d"} {
		issues = append(issues, e.issue(t, title, ""))
	}
	for _, i := range issues[:3] {
		if err := e.svc.PinIssue(ctx, e.owner, e.repoName, i.Number, e.ownerID); err != nil {
			t.Fatalf("pin %d: %v", i.Number, err)
		}
	}
	err := e.svc.PinIssue(ctx, e.owner, e.repoName, issues[3].Number, e.ownerID)
	if err == nil || !strings.Contains(err.Error(), "more than 3 pinned") {
		t.Fatalf("4th pin: err = %v, want pin limit", err)
	}
	if err := e.svc.PinIssue(ctx, e.owner, e.repoName, issues[0].Number, e.ownerID); err != nil {
		t.Errorf("re-pinning an already pinned issue at the limit must be a no-op, got %v", err)
	}

	pinned, err := e.svc.ListPinned(ctx, e.owner, e.repoName, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 3 {
		t.Errorf("ListPinned = %d, want 3", len(pinned))
	}
}

func TestIssueService_LinkPullAndLinkedQueries(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	i := e.issue(t, "linked", "")
	pr := &model.PullRequest{RepoID: e.repoID, AuthorID: e.ownerID, Title: "pr", State: model.PRStateOpen, HeadBranch: "f", BaseBranch: "main"}
	if err := store.NewPullStore(e.db).Create(ctx, pr); err != nil {
		t.Fatal(err)
	}

	if err := e.svc.LinkPull(ctx, pr.ID, i.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.LinkPull(ctx, pr.ID, i.ID); err != nil {
		t.Errorf("relinking: %v", err)
	}
	linked, err := e.svc.LinkedForPull(ctx, pr.ID, nil)
	if err != nil || len(linked) != 1 || linked[0].ID != i.ID {
		t.Fatalf("LinkedForPull = %v, %v", linked, err)
	}
	prs, err := e.svc.LinkedPRs(ctx, e.owner, e.repoName, i.Number, nil)
	if err != nil || len(prs) != 1 || prs[0].ID != pr.ID {
		t.Fatalf("LinkedPRs = %v, %v", prs, err)
	}

	if err := e.svc.UnlinkPull(ctx, pr.ID, i.ID); err != nil {
		t.Fatal(err)
	}
	if linked, _ := e.svc.LinkedForPull(ctx, pr.ID, nil); len(linked) != 0 {
		t.Errorf("after unlink: %d linked, want 0", len(linked))
	}
}

func TestIssueService_UserListsAndCounts(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	alice := e.writerID

	assigned := e.issue(t, "assigned", "")
	mentioned := e.issue(t, "mentioned", "")
	closedAssigned := e.issue(t, "closed assigned", "")
	testutil.Exec(t, e.db, `INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2), ($3, $2)`, assigned.ID, alice, closedAssigned.ID)
	if _, err := e.svc.SetState(ctx, e.owner, e.repoName, closedAssigned.Number, model.IssueStateClosed, e.ownerID, "owner"); err != nil {
		t.Fatal(err)
	}
	var commentID int64
	if err := e.db.QueryRowContext(ctx,
		`INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, 'hi') RETURNING id`,
		e.repoID, mentioned.ID, e.ownerID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `INSERT INTO mentions (comment_id, user_id) VALUES ($1, $2)`, commentID, alice)

	titles := func(items []store.IssueListItem) string {
		var out []string
		for _, it := range items {
			out = append(out, it.Title)
		}
		return strings.Join(out, ",")
	}
	tests := []struct {
		name        string
		mode, state string
		want        string
	}{
		{"assigned open", "assigned", "open", "assigned"},
		{"unknown mode falls back to assigned", "bogus", "open", "assigned"},
		{"assigned closed", "assigned", "closed", "closed assigned"},
		{"unknown state falls back to open", "assigned", "weird", "assigned"},
		{"mentioned", "mentioned", "open", "mentioned"},
		{"created by alice", "created", "open", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := e.svc.ListForUser(ctx, alice, tt.mode, tt.state)
			if err != nil {
				t.Fatal(err)
			}
			if got := titles(items); got != tt.want {
				t.Errorf("titles = %q, want %q", got, tt.want)
			}
		})
	}
	if items, err := e.svc.ListForUser(ctx, e.ownerID, "created", "open"); err != nil || len(items) != 2 {
		t.Errorf("owner created open = %d, %v; want 2", len(items), err)
	}

	counts, err := e.svc.CountsForUser(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if counts["assigned:open"] != 1 || counts["assigned:closed"] != 1 || counts["mentioned:open"] != 1 || counts["created:open"] != 0 {
		t.Errorf("counts = %v", counts)
	}

	if n, err := e.svc.CountOpenAssignedTo(ctx, alice); err != nil || n != 1 {
		t.Errorf("CountOpenAssignedTo = %d, %v; want 1", n, err)
	}
	if n, err := e.svc.CountDueThisWeekAssignedTo(ctx, alice); err != nil || n != 0 {
		t.Errorf("CountDueThisWeekAssignedTo (no milestone) = %d, %v; want 0", n, err)
	}
	var msID int64
	if err := e.db.QueryRowContext(ctx,
		`INSERT INTO milestones (repo_id, number, title, due_date) VALUES ($1, 1, 'm', NOW() + INTERVAL '2 days') RETURNING id`,
		e.repoID).Scan(&msID); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `UPDATE issues SET milestone_id = $1 WHERE id = $2`, msID, assigned.ID)
	if n, err := e.svc.CountDueThisWeekAssignedTo(ctx, alice); err != nil || n != 1 {
		t.Errorf("CountDueThisWeekAssignedTo = %d, %v; want 1", n, err)
	}
}

func TestIssueService_RepoStats(t *testing.T) {
	e := newIssueLifeEnv(t)
	ctx := context.Background()
	e.issue(t, "one", "")
	e.issue(t, "two", "")
	closed := e.issue(t, "three", "")
	if _, err := e.svc.SetState(ctx, e.owner, e.repoName, closed.Number, model.IssueStateClosed, e.ownerID, "owner"); err != nil {
		t.Fatal(err)
	}
	e.issue(t, "hidden", "private")

	since := time.Now().Add(-time.Hour)
	if n, err := e.svc.CountCreatedSince(ctx, e.repoID, since); err != nil || n != 4 {
		t.Errorf("CountCreatedSince = %d, %v; want 4", n, err)
	}
	if n, err := e.svc.CountCreatedSince(ctx, e.repoID, time.Now().Add(time.Hour)); err != nil || n != 0 {
		t.Errorf("CountCreatedSince(future) = %d, %v; want 0", n, err)
	}
	if n, err := e.svc.CountClosedSince(ctx, e.repoID, since); err != nil || n != 1 {
		t.Errorf("CountClosedSince = %d, %v; want 1", n, err)
	}
	if n, err := e.svc.CountOpen(ctx, e.repoID, nil); err != nil || n != 2 {
		t.Errorf("CountOpen(anon) = %d, %v; want 2 (private issue hidden)", n, err)
	}
	if n, err := e.svc.CountOpen(ctx, e.repoID, &e.ownerID); err != nil || n != 3 {
		t.Errorf("CountOpen(owner) = %d, %v; want 3", n, err)
	}

	weekly, err := e.svc.WeeklyCreated(ctx, e.repoID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(weekly) != 4 || weekly[3] != 4 {
		t.Errorf("WeeklyCreated = %v, want 4 buckets ending in 4", weekly)
	}
}
