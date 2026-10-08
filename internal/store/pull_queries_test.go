package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type pullFixture struct {
	db      *sql.DB
	ps      *store.PullStore
	repoID  int64
	ownerID int64
	otherID int64
	suffix  string
}

func newPullFixture(t *testing.T) pullFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix+"_o")
	otherID := testutil.SeedUser(t, db, suffix+"_x")
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix+"_o", suffix)
	return pullFixture{db: db, ps: store.NewPullStore(db), repoID: repoID, ownerID: ownerID, otherID: otherID, suffix: suffix}
}

func (f pullFixture) create(t *testing.T, title, head string, state model.PRState) *model.PullRequest {
	t.Helper()
	pr := &model.PullRequest{RepoID: f.repoID, AuthorID: f.ownerID, Title: title, Body: "body " + title, HeadBranch: head, BaseBranch: "main", State: state}
	if err := f.ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create %s: %v", title, err)
	}
	return pr
}

func (f pullFixture) assign(t *testing.T, pullID, userID int64) {
	t.Helper()
	testutil.Exec(t, f.db, `INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`, pullID, userID)
}

func prNumbers(prs []model.PullRequest) []int {
	out := make([]int, len(prs))
	for i, p := range prs {
		out[i] = p.Number
	}
	return out
}

func TestPullStore_Create_NumbersPerRepoAndHeadSHA(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	other := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+f.suffix+"_o", f.suffix+"_2")

	p1 := f.create(t, "one", "b1", model.PRStateOpen)
	p2 := &model.PullRequest{RepoID: f.repoID, AuthorID: f.ownerID, Title: "two", HeadBranch: "b2", BaseBranch: "main", State: model.PRStateOpen, HeadSHA: "abc123", IsDraft: true}
	if err := f.ps.Create(ctx, p2); err != nil {
		t.Fatalf("Create: %v", err)
	}
	p3 := &model.PullRequest{RepoID: other, AuthorID: f.ownerID, Title: "elsewhere", HeadBranch: "b", BaseBranch: "main", State: model.PRStateOpen}
	if err := f.ps.Create(ctx, p3); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p1.Number != 1 || p2.Number != 2 || p3.Number != 1 {
		t.Errorf("numbers = %d, %d, %d; want 1, 2, 1", p1.Number, p2.Number, p3.Number)
	}

	got, err := f.ps.GetByNumber(ctx, f.repoID, 2)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if got.HeadSHA != "abc123" || !got.IsDraft {
		t.Errorf("HeadSHA/IsDraft = %q/%v", got.HeadSHA, got.IsDraft)
	}
	first, _ := f.ps.GetByNumber(ctx, f.repoID, 1)
	if first.HeadSHA != "" || first.IsDraft || first.DraftAt != nil || first.MergedAt != nil || first.ClosedAt != nil || first.AutoMergeBy != nil {
		t.Errorf("unexpected optional fields on plain PR: %+v", first)
	}
}

func TestPullStore_GetByID(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	pr := f.create(t, "by id", "feat", model.PRStateOpen)

	got, err := f.ps.GetByID(ctx, pr.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "by id" || got.RepoID != f.repoID || got.Number != pr.Number || got.HeadBranch != "feat" {
		t.Errorf("got %+v", got)
	}
	if _, err := f.ps.GetByID(ctx, pr.ID+1_000_000); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown id: %v, want sql.ErrNoRows", err)
	}
}

func TestPullStore_UpdateState(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()

	merged := f.create(t, "merge me", "m", model.PRStateOpen)
	closed := f.create(t, "close me", "c", model.PRStateOpen)
	reopened := f.create(t, "reopen me", "r", model.PRStateOpen)
	for id, state := range map[int64]model.PRState{merged.ID: model.PRStateMerged, closed.ID: model.PRStateClosed, reopened.ID: model.PRStateClosed} {
		if err := f.ps.UpdateState(ctx, id, state); err != nil {
			t.Fatalf("UpdateState %s: %v", state, err)
		}
	}
	if err := f.ps.UpdateState(ctx, reopened.ID, model.PRStateOpen); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	tests := []struct {
		name       string
		id         int64
		wantState  model.PRState
		wantMerged bool
		wantClosed bool
	}{
		{"merged sets merged_at only", merged.ID, model.PRStateMerged, true, false},
		{"closed sets closed_at only", closed.ID, model.PRStateClosed, false, true},
		{"reopen keeps the earlier closed_at", reopened.ID, model.PRStateOpen, false, true},
	}
	for _, tc := range tests {
		got, err := f.ps.GetByID(ctx, tc.id)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.State != tc.wantState || (got.MergedAt != nil) != tc.wantMerged || (got.ClosedAt != nil) != tc.wantClosed {
			t.Errorf("%s: state=%s mergedAt=%v closedAt=%v", tc.name, got.State, got.MergedAt, got.ClosedAt)
		}
	}
}

func TestPullStore_SetDraft_KeepsDraftAtWhenMarkedReady(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	pr := f.create(t, "draft", "d", model.PRStateOpen)

	if err := f.ps.SetDraft(ctx, pr.ID, true); err != nil {
		t.Fatalf("SetDraft(true): %v", err)
	}
	drafted, _ := f.ps.GetByID(ctx, pr.ID)
	if err := f.ps.SetDraft(ctx, pr.ID, false); err != nil {
		t.Fatalf("SetDraft(false): %v", err)
	}
	ready, _ := f.ps.GetByID(ctx, pr.ID)
	if ready.IsDraft || ready.DraftAt == nil || !ready.DraftAt.Equal(*drafted.DraftAt) {
		t.Errorf("after ready: IsDraft=%v DraftAt=%v, want false and %v", ready.IsDraft, ready.DraftAt, drafted.DraftAt)
	}
}

func TestPullStore_UpdateTitleAndBody(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	pr := f.create(t, "old title", "t", model.PRStateOpen)

	if err := f.ps.UpdateTitle(ctx, pr.ID, "new title"); err != nil {
		t.Fatalf("UpdateTitle: %v", err)
	}
	if err := f.ps.UpdateBody(ctx, pr.ID, "new body"); err != nil {
		t.Fatalf("UpdateBody: %v", err)
	}
	got, _ := f.ps.GetByID(ctx, pr.ID)
	if got.Title != "new title" || got.Body != "new body" || !got.UpdatedAt.After(pr.UpdatedAt) {
		t.Errorf("got %+v", got)
	}
}

func TestPullStore_SetAutoMerge(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	pr := f.create(t, "auto", "a", model.PRStateOpen)

	if err := f.ps.SetAutoMerge(ctx, pr.ID, true, "squash", &f.otherID); err != nil {
		t.Fatalf("arm: %v", err)
	}
	armed, _ := f.ps.GetByNumber(ctx, f.repoID, pr.Number)
	if !armed.AutoMergeEnabled || armed.AutoMergeStrategy != "squash" || armed.AutoMergeBy == nil || *armed.AutoMergeBy != f.otherID {
		t.Errorf("armed = %+v", armed)
	}

	if err := f.ps.SetAutoMerge(ctx, pr.ID, false, "", &f.otherID); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	disarmed, _ := f.ps.GetByID(ctx, pr.ID)
	if disarmed.AutoMergeEnabled || disarmed.AutoMergeStrategy != "" || disarmed.AutoMergeBy != nil {
		t.Errorf("disarmed = %+v; byUserID must be dropped when disabling", disarmed)
	}
}

func TestPullStore_Lists(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	open1 := f.create(t, "open1", "o1", model.PRStateOpen)
	closed := f.create(t, "closed", "c", model.PRStateOpen)
	merged := f.create(t, "merged", "m", model.PRStateOpen)
	f.create(t, "open2", "o2", model.PRStateOpen)
	if err := f.ps.UpdateState(ctx, closed.ID, model.PRStateClosed); err != nil {
		t.Fatal(err)
	}
	if err := f.ps.UpdateState(ctx, merged.ID, model.PRStateMerged); err != nil {
		t.Fatal(err)
	}

	t.Run("List returns every state newest first with author name", func(t *testing.T) {
		got, err := f.ps.List(ctx, f.repoID)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if want := []int{4, 3, 2, 1}; !slices.Equal(prNumbers(got), want) {
			t.Errorf("numbers = %v, want %v", prNumbers(got), want)
		}
		if got[0].AuthorName == "" {
			t.Error("AuthorName not populated")
		}
	})

	t.Run("List unknown repo is empty", func(t *testing.T) {
		got, err := f.ps.List(ctx, f.repoID+1_000_000)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got %v, %v; want empty non-nil", got, err)
		}
	})

	t.Run("ListByState", func(t *testing.T) {
		tests := []struct {
			name          string
			state         model.PRState
			offset, limit int
			want          []int
		}{
			{"open", model.PRStateOpen, 0, 0, []int{4, 1}},
			{"closed", model.PRStateClosed, 0, 0, []int{2}},
			{"merged", model.PRStateMerged, 0, 0, []int{3}},
			{"empty state means all", "", 0, 0, []int{4, 3, 2, 1}},
			{"limit", "", 0, 2, []int{4, 3}},
			{"offset+limit", "", 2, 1, []int{2}},
			{"offset past end", "", 10, 5, []int{}},
		}
		for _, tc := range tests {
			got, err := f.ps.ListByState(ctx, f.repoID, tc.state, tc.offset, tc.limit)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !slices.Equal(prNumbers(got), tc.want) {
				t.Errorf("%s: got %v, want %v", tc.name, prNumbers(got), tc.want)
			}
		}
	})

	t.Run("ListOpen", func(t *testing.T) {
		got, err := f.ps.ListOpen(ctx, f.repoID)
		if err != nil {
			t.Fatalf("ListOpen: %v", err)
		}
		if want := []int{4, 1}; !slices.Equal(prNumbers(got), want) {
			t.Errorf("got %v, want %v", prNumbers(got), want)
		}
	})

	t.Run("GetManyByIDs", func(t *testing.T) {
		if got, err := f.ps.GetManyByIDs(ctx, nil); err != nil || got != nil {
			t.Errorf("empty = %v, %v; want nil, nil", got, err)
		}
		got, err := f.ps.GetManyByIDs(ctx, []int64{open1.ID, merged.ID, open1.ID + 9_000_000})
		if err != nil {
			t.Fatalf("GetManyByIDs: %v", err)
		}
		ids := []int64{}
		for _, p := range got {
			ids = append(ids, p.ID)
		}
		slices.Sort(ids)
		if want := []int64{open1.ID, merged.ID}; !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v (unknown id skipped)", ids, want)
		}
	})
}

func TestPullStore_Counts(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	repo2 := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+f.suffix+"_o", suffix)
	emptyRepo := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+f.suffix+"_o", suffix+"_e")

	a := f.create(t, "a", "a", model.PRStateOpen)
	b := f.create(t, "b", "b", model.PRStateOpen)
	c := f.create(t, "c", "c", model.PRStateOpen)
	f.create(t, "d", "d", model.PRStateOpen)
	if err := f.ps.UpdateState(ctx, b.ID, model.PRStateMerged); err != nil {
		t.Fatal(err)
	}
	if err := f.ps.UpdateState(ctx, c.ID, model.PRStateClosed); err != nil {
		t.Fatal(err)
	}
	other := &model.PullRequest{RepoID: repo2, AuthorID: f.ownerID, Title: "r2", HeadBranch: "x", BaseBranch: "main", State: model.PRStateOpen}
	if err := f.ps.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, f.db, `UPDATE pull_requests SET created_at = NOW() - interval '30 days' WHERE id = $1`, a.ID)
	f.assign(t, a.ID, f.otherID)
	f.assign(t, other.ID, f.otherID)

	since := time.Now().Add(-7 * 24 * time.Hour)

	if n, err := f.ps.CountCreatedSince(ctx, f.repoID, since); err != nil || n != 3 {
		t.Errorf("CountCreatedSince = %d, %v; want 3 (old one excluded)", n, err)
	}
	if n, err := f.ps.CountMergedSince(ctx, f.repoID, since); err != nil || n != 1 {
		t.Errorf("CountMergedSince = %d, %v; want 1", n, err)
	}
	if n, err := f.ps.CountMergedSince(ctx, f.repoID, time.Now().Add(time.Hour)); err != nil || n != 0 {
		t.Errorf("CountMergedSince future = %d, %v; want 0", n, err)
	}
	if n, err := f.ps.CountOpen(ctx, f.repoID); err != nil || n != 2 {
		t.Errorf("CountOpen = %d, %v; want 2", n, err)
	}

	t.Run("CountOpenByRepoIDs", func(t *testing.T) {
		if got, err := f.ps.CountOpenByRepoIDs(ctx, nil); err != nil || got == nil || len(got) != 0 {
			t.Errorf("empty = %v, %v; want empty non-nil", got, err)
		}
		got, err := f.ps.CountOpenByRepoIDs(ctx, []int64{f.repoID, repo2, emptyRepo})
		if err != nil {
			t.Fatalf("CountOpenByRepoIDs: %v", err)
		}
		if got[f.repoID] != 2 || got[repo2] != 1 {
			t.Errorf("got %v", got)
		}
		if _, ok := got[emptyRepo]; ok {
			t.Error("repo without open PRs must be absent")
		}
	})

	t.Run("CountOpenAssignedTo skips soft-deleted repos", func(t *testing.T) {
		if n, err := f.ps.CountOpenAssignedTo(ctx, f.otherID); err != nil || n != 2 {
			t.Fatalf("CountOpenAssignedTo = %d, %v; want 2", n, err)
		}
		testutil.Exec(t, f.db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, repo2)
		if n, err := f.ps.CountOpenAssignedTo(ctx, f.otherID); err != nil || n != 1 {
			t.Errorf("after soft delete = %d, %v; want 1", n, err)
		}
	})
}

func TestPullStore_WeeklyCreated(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	old := f.create(t, "old", "o", model.PRStateOpen)
	f.create(t, "new1", "n1", model.PRStateOpen)
	f.create(t, "new2", "n2", model.PRStateOpen)
	testutil.Exec(t, f.db, `UPDATE pull_requests SET created_at = NOW() - interval '21 days' WHERE id = $1`, old.ID)

	got, err := f.ps.WeeklyCreated(ctx, f.repoID, 6)
	if err != nil {
		t.Fatalf("WeeklyCreated: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("len = %d, want 6", len(got))
	}
	sum := 0
	for _, n := range got {
		sum += n
	}
	if sum != 3 || got[5] != 2 {
		t.Errorf("buckets = %v; want total 3 with 2 in the current week", got)
	}
	for in, want := range map[int]int{0: 1, 999: 104} {
		b, err := f.ps.WeeklyCreated(ctx, f.repoID, in)
		if err != nil || len(b) != want {
			t.Errorf("WeeklyCreated(%d) len = %d, %v; want %d", in, len(b), err, want)
		}
	}
}

func TestPullStore_ListForUserAndByIDs_Visibility(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	privateRepo := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+f.suffix+"_o", suffix)
	testutil.Exec(t, f.db, `UPDATE repositories SET private = TRUE WHERE id = $1`, privateRepo)
	deletedRepo := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+f.suffix+"_o", suffix+"_d")

	public := f.create(t, "public pr", "p", model.PRStateOpen)
	closed := f.create(t, "closed pr", "c", model.PRStateOpen)
	if err := f.ps.UpdateState(ctx, closed.ID, model.PRStateClosed); err != nil {
		t.Fatal(err)
	}
	hidden := &model.PullRequest{RepoID: privateRepo, AuthorID: f.ownerID, Title: "hidden pr", HeadBranch: "h", BaseBranch: "main", State: model.PRStateOpen}
	gone := &model.PullRequest{RepoID: deletedRepo, AuthorID: f.ownerID, Title: "gone pr", HeadBranch: "g", BaseBranch: "main", State: model.PRStateOpen}
	for _, pr := range []*model.PullRequest{hidden, gone} {
		if err := f.ps.Create(ctx, pr); err != nil {
			t.Fatal(err)
		}
	}
	testutil.Exec(t, f.db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, deletedRepo)
	f.assign(t, public.ID, f.otherID)
	f.assign(t, hidden.ID, f.otherID)
	testutil.Exec(t, f.db, `UPDATE pull_requests SET updated_at = NOW() - interval '1 hour' WHERE id = $1`, public.ID)

	titles := func(items []store.PullListItem) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.Title)
		}
		return out
	}

	t.Run("created by owner: open", func(t *testing.T) {
		got, err := f.ps.ListForUser(ctx, f.ownerID, "created", "open")
		if err != nil {
			t.Fatalf("ListForUser: %v", err)
		}
		if want := []string{"hidden pr", "public pr"}; !slices.Equal(titles(got), want) {
			t.Errorf("titles = %v, want %v (newest update first, deleted repo excluded)", titles(got), want)
		}
		if got[1].RepoFullName != "testuser_"+f.suffix+"_o/testrepo_"+f.suffix || got[1].AuthorID != f.ownerID || got[1].Number != public.Number {
			t.Errorf("item = %+v", got[1])
		}
	})

	t.Run("created by owner: closed", func(t *testing.T) {
		got, err := f.ps.ListForUser(ctx, f.ownerID, "created", "closed")
		if err != nil || !slices.Equal(titles(got), []string{"closed pr"}) {
			t.Errorf("got %v, %v", titles(got), err)
		}
	})

	t.Run("assigned: private repo hidden from a user without access", func(t *testing.T) {
		got, err := f.ps.ListForUser(ctx, f.otherID, "assigned", "open")
		if err != nil {
			t.Fatalf("ListForUser: %v", err)
		}
		if want := []string{"public pr"}; !slices.Equal(titles(got), want) {
			t.Errorf("titles = %v, want %v", titles(got), want)
		}
	})

	t.Run("assigned: access via permission row", func(t *testing.T) {
		testutil.Exec(t, f.db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'reader')`, privateRepo, f.otherID)
		got, err := f.ps.ListForUser(ctx, f.otherID, "assigned", "open")
		if err != nil {
			t.Fatalf("ListForUser: %v", err)
		}
		if want := []string{"hidden pr", "public pr"}; !slices.Equal(titles(got), want) {
			t.Errorf("titles = %v, want %v", titles(got), want)
		}
	})

	t.Run("ListByIDs", func(t *testing.T) {
		if got, err := f.ps.ListByIDs(ctx, f.ownerID, nil, "open"); err != nil || got == nil || len(got) != 0 {
			t.Errorf("empty ids = %v, %v; want empty non-nil", got, err)
		}
		ids := []int64{public.ID, closed.ID, hidden.ID, gone.ID}
		got, err := f.ps.ListByIDs(ctx, f.ownerID, ids, "open")
		if err != nil {
			t.Fatalf("ListByIDs: %v", err)
		}
		if want := []string{"hidden pr", "public pr"}; !slices.Equal(titles(got), want) {
			t.Errorf("owner open = %v, want %v (closed, soft-deleted excluded)", titles(got), want)
		}
		stranger := testutil.SeedUser(t, f.db, f.suffix+"_s")
		got, err = f.ps.ListByIDs(ctx, stranger, ids, "open")
		if err != nil {
			t.Fatalf("ListByIDs stranger: %v", err)
		}
		if want := []string{"public pr"}; !slices.Equal(titles(got), want) {
			t.Errorf("stranger open = %v, want %v (private repo excluded)", titles(got), want)
		}
	})
}

func TestPullStore_AssignedAtForUser(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	openPR := f.create(t, "open", "o", model.PRStateOpen)
	closedPR := f.create(t, "closed", "c", model.PRStateOpen)
	if err := f.ps.UpdateState(ctx, closedPR.ID, model.PRStateClosed); err != nil {
		t.Fatal(err)
	}
	f.assign(t, openPR.ID, f.otherID)
	f.assign(t, closedPR.ID, f.otherID)
	testutil.Exec(t, f.db, `UPDATE pull_assignees SET created_at = $1 WHERE pull_id = $2`, time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC), openPR.ID)

	got, err := f.ps.AssignedAtForUser(ctx, f.otherID)
	if err != nil {
		t.Fatalf("AssignedAtForUser: %v", err)
	}
	if len(got) != 1 || !got[openPR.ID].Equal(time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)) {
		t.Errorf("got %v, want only the open PR's assignment time", got)
	}
	if got, err := f.ps.AssignedAtForUser(ctx, f.ownerID); err != nil || len(got) != 0 {
		t.Errorf("unassigned user = %v, %v; want empty", got, err)
	}
}

func TestPullStore_UpdateHeadSHA(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	a := f.create(t, "a", "feature", model.PRStateOpen)
	b := f.create(t, "b", "feature", model.PRStateOpen)
	closed := f.create(t, "closed", "feature", model.PRStateOpen)
	elsewhere := f.create(t, "other branch", "other", model.PRStateOpen)
	if err := f.ps.UpdateState(ctx, closed.ID, model.PRStateClosed); err != nil {
		t.Fatal(err)
	}

	if err := f.ps.UpdateHeadSHA(ctx, elsewhere.ID, "sha-one"); err != nil {
		t.Fatalf("UpdateHeadSHA: %v", err)
	}
	if err := f.ps.UpdateHeadSHAByBranch(ctx, f.repoID, "feature", "sha-two"); err != nil {
		t.Fatalf("UpdateHeadSHAByBranch: %v", err)
	}

	want := map[int64]string{a.ID: "sha-two", b.ID: "sha-two", closed.ID: "", elsewhere.ID: "sha-one"}
	for id, sha := range want {
		got, _ := f.ps.GetByID(ctx, id)
		if got.HeadSHA != sha {
			t.Errorf("pull %d HeadSHA = %q, want %q", id, got.HeadSHA, sha)
		}
	}
}

func TestPullStore_ListLinkedToIssue_Visibility(t *testing.T) {
	f := newPullFixture(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	privateRepo := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+f.suffix+"_o", suffix)
	testutil.Exec(t, f.db, `UPDATE repositories SET private = TRUE WHERE id = $1`, privateRepo)

	issueID := seedIssueRow(t, f.db, f.repoID, f.ownerID, 1, "open")
	local := f.create(t, "same repo", "l", model.PRStateOpen)
	remote := &model.PullRequest{RepoID: privateRepo, AuthorID: f.ownerID, Title: "private repo", HeadBranch: "r", BaseBranch: "main", State: model.PRStateOpen}
	if err := f.ps.Create(ctx, remote); err != nil {
		t.Fatal(err)
	}
	for _, pr := range []*model.PullRequest{remote, local} {
		testutil.Exec(t, f.db, `INSERT INTO pull_issue_links (pull_id, issue_id) VALUES ($1, $2)`, pr.ID, issueID)
	}

	titles := func(prs []model.PullRequest) []string {
		out := []string{}
		for _, p := range prs {
			out = append(out, p.Title)
		}
		return out
	}

	t.Run("anonymous sees public repos only", func(t *testing.T) {
		got, err := f.ps.ListLinkedToIssue(ctx, issueID, nil)
		if err != nil {
			t.Fatalf("ListLinkedToIssue: %v", err)
		}
		if want := []string{"same repo"}; !slices.Equal(titles(got), want) {
			t.Errorf("titles = %v, want %v", titles(got), want)
		}
		if got[0].RepoOwner != "testuser_"+f.suffix+"_o" || got[0].RepoName != "testrepo_"+f.suffix {
			t.Errorf("RepoOwner/RepoName = %q/%q", got[0].RepoOwner, got[0].RepoName)
		}
	})

	t.Run("owner sees both, the issue's own repo first", func(t *testing.T) {
		got, err := f.ps.ListLinkedToIssue(ctx, issueID, &f.ownerID)
		if err != nil {
			t.Fatalf("ListLinkedToIssue: %v", err)
		}
		if want := []string{"same repo", "private repo"}; !slices.Equal(titles(got), want) {
			t.Errorf("titles = %v, want %v", titles(got), want)
		}
	})

	t.Run("issue without links", func(t *testing.T) {
		lone := seedIssueRow(t, f.db, f.repoID, f.ownerID, 2, "open")
		got, err := f.ps.ListLinkedToIssue(ctx, lone, &f.ownerID)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got %v, %v; want empty non-nil", got, err)
		}
	})
}
