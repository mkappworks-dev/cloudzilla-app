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

type milestoneFixture struct {
	db      *sql.DB
	ms      *store.MilestoneStore
	repoID  int64
	ownerID int64
	otherID int64
}

func newMilestoneFixture(t *testing.T) milestoneFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix+"_o")
	otherID := testutil.SeedUser(t, db, suffix+"_x")
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix+"_o", suffix)
	return milestoneFixture{db: db, ms: store.NewMilestoneStore(db), repoID: repoID, ownerID: ownerID, otherID: otherID}
}

func (f milestoneFixture) create(t *testing.T, title string) *model.Milestone {
	t.Helper()
	m := &model.Milestone{RepoID: f.repoID, Title: title, Description: "desc " + title}
	if err := f.ms.Create(context.Background(), m); err != nil {
		t.Fatalf("Create %s: %v", title, err)
	}
	return m
}

func (f milestoneFixture) issue(t *testing.T, number int, authorID, milestoneID int64, state, visibility string) int64 {
	t.Helper()
	var id int64
	err := f.db.QueryRowContext(context.Background(),
		`INSERT INTO issues (repo_id, number, author_id, title, state, visibility, milestone_id)
		 VALUES ($1, $2, $3, 'i', $4, $5, $6) RETURNING id`,
		f.repoID, number, authorID, state, visibility, milestoneID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, f.db, `DELETE FROM issues WHERE id = $1`, id) })
	return id
}

func (f milestoneFixture) pull(t *testing.T, number int, state string, milestoneID int64) int64 {
	t.Helper()
	id := seedPullRow(t, f.db, f.repoID, f.ownerID, number, state)
	testutil.Exec(t, f.db, `UPDATE pull_requests SET milestone_id = $1 WHERE id = $2`, milestoneID, id)
	return id
}

func TestMilestoneStore_Create_NumbersPerRepo(t *testing.T) {
	f := newMilestoneFixture(t)
	suffix := testutil.UniqueSuffix(t)
	otherRepo := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+suffix, suffix)
	ctx := context.Background()

	m1, m2 := f.create(t, "v1"), f.create(t, "v2")
	m3 := &model.Milestone{RepoID: otherRepo, Title: "first elsewhere"}
	if err := f.ms.Create(ctx, m3); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m1.Number != 1 || m2.Number != 2 || m3.Number != 1 {
		t.Errorf("numbers = %d, %d, %d; want 1, 2, 1", m1.Number, m2.Number, m3.Number)
	}

	if err := f.ms.Delete(ctx, f.repoID, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	m4 := f.create(t, "v3")
	if m4.Number != 3 {
		t.Errorf("number after deleting #1 = %d, want 3 (MAX+1 never reuses a lower gap)", m4.Number)
	}
}

func TestMilestoneStore_Create_DueDateRoundTrip(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	due := time.Date(2031, 4, 5, 6, 7, 8, 0, time.UTC)
	m := &model.Milestone{RepoID: f.repoID, Title: "dated", DueDate: &due}
	if err := f.ms.Create(ctx, m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.ms.GetByNumber(ctx, f.repoID, m.Number, nil)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) {
		t.Errorf("DueDate = %v, want %v", got.DueDate, due)
	}
	if got.State != "open" || got.ClosedAt != nil {
		t.Errorf("State/ClosedAt = %q/%v, want open/nil", got.State, got.ClosedAt)
	}
}

func TestMilestoneStore_Get_NotFound(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "only")

	if _, err := f.ms.GetByNumber(ctx, f.repoID, m.Number+99, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetByNumber unknown number: %v, want sql.ErrNoRows", err)
	}
	if _, err := f.ms.GetByID(ctx, m.ID+1_000_000, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetByID unknown id: %v, want sql.ErrNoRows", err)
	}
	got, err := f.ms.GetByID(ctx, m.ID, nil)
	if err != nil || got.Title != "only" {
		t.Errorf("GetByID = %+v, %v", got, err)
	}
}

func TestMilestoneStore_Update(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "old")
	before := m.UpdatedAt

	due, closed := time.Date(2032, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2032, 1, 3, 0, 0, 0, 0, time.UTC)
	m.Title, m.Description, m.State, m.DueDate, m.ClosedAt = "new", "changed", "closed", &due, &closed
	if err := f.ms.Update(ctx, m); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !m.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt %v not after %v", m.UpdatedAt, before)
	}

	got, err := f.ms.GetByID(ctx, m.ID, nil)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "new" || got.Description != "changed" || got.State != "closed" {
		t.Errorf("got %+v", got)
	}
	if got.DueDate == nil || !got.DueDate.Equal(due) || got.ClosedAt == nil || !got.ClosedAt.Equal(closed) {
		t.Errorf("DueDate/ClosedAt = %v/%v", got.DueDate, got.ClosedAt)
	}

	got.DueDate, got.ClosedAt, got.State = nil, nil, "open"
	if err := f.ms.Update(ctx, got); err != nil {
		t.Fatalf("Update clearing dates: %v", err)
	}
	cleared, _ := f.ms.GetByID(ctx, m.ID, nil)
	if cleared.DueDate != nil || cleared.ClosedAt != nil {
		t.Errorf("dates not cleared: %v/%v", cleared.DueDate, cleared.ClosedAt)
	}
}

func TestMilestoneStore_Update_WrongRepoMatchesNothing(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "keep")

	forged := *m
	forged.RepoID = f.repoID + 1_000_000
	forged.Title = "hijacked"
	if err := f.ms.Update(ctx, &forged); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Update with wrong repo: %v, want sql.ErrNoRows", err)
	}
	got, _ := f.ms.GetByID(ctx, m.ID, nil)
	if got.Title != "keep" {
		t.Errorf("title = %q, want keep", got.Title)
	}
}

func TestMilestoneStore_Delete_ClearsIssueMilestone(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "doomed")
	issueID := f.issue(t, 1, f.ownerID, m.ID, "open", "public")

	if err := f.ms.Delete(ctx, f.repoID, m.Number); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.ms.GetByID(ctx, m.ID, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("milestone still present: %v", err)
	}
	mid, err := f.ms.GetIssueID(ctx, issueID)
	if err != nil || mid != nil {
		t.Errorf("issue milestone after delete = %v, %v; want nil", mid, err)
	}
	if err := f.ms.Delete(ctx, f.repoID, m.Number); err != nil {
		t.Errorf("deleting absent milestone: %v", err)
	}
}

func TestMilestoneStore_Counts_RespectIssueVisibility(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "counts")
	f.issue(t, 1, f.otherID, m.ID, "open", "public")
	f.issue(t, 2, f.otherID, m.ID, "open", "private")
	f.issue(t, 3, f.otherID, m.ID, "closed", "public")
	f.issue(t, 4, f.otherID, m.ID, "closed", "private")
	f.issue(t, 5, f.ownerID, m.ID, "open", "private")

	tests := []struct {
		name         string
		viewer       *int64
		wantOpen     int
		wantClosed   int
		wantOpenOnly int
	}{
		{"anonymous sees public only", nil, 1, 1, 1},
		{"author sees own private", &f.otherID, 2, 2, 2},
		{"repo owner sees all", &f.ownerID, 3, 2, 3},
	}
	for _, tc := range tests {
		got, err := f.ms.GetByID(ctx, m.ID, tc.viewer)
		if err != nil {
			t.Fatalf("%s GetByID: %v", tc.name, err)
		}
		if got.OpenCount != tc.wantOpen || got.ClosedCount != tc.wantClosed {
			t.Errorf("%s GetByID: open/closed = %d/%d, want %d/%d", tc.name, got.OpenCount, got.ClosedCount, tc.wantOpen, tc.wantClosed)
		}
		list, err := f.ms.ListByRepo(ctx, f.repoID, tc.viewer)
		if err != nil || len(list) != 1 {
			t.Fatalf("%s ListByRepo: %v, %v", tc.name, list, err)
		}
		if list[0].OpenCount != tc.wantOpen || list[0].ClosedCount != tc.wantClosed {
			t.Errorf("%s ListByRepo: open/closed = %d/%d", tc.name, list[0].OpenCount, list[0].ClosedCount)
		}
	}
}

func TestMilestoneStore_ListByRepo_NewestFirst(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	a, b := f.create(t, "a"), f.create(t, "b")
	testutil.Exec(t, f.db, `UPDATE milestones SET created_at = NOW() - interval '1 day' WHERE id = $1`, a.ID)

	got, err := f.ms.ListByRepo(ctx, f.repoID, nil)
	if err != nil {
		t.Fatalf("ListByRepo: %v", err)
	}
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("order = %+v, want b then a", got)
	}
}

func TestMilestoneStore_SetPull(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	otherRepo := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+suffix, suffix)
	foreign := &model.Milestone{RepoID: otherRepo, Title: "foreign"}
	if err := f.ms.Create(ctx, foreign); err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := f.create(t, "local")
	pullID := seedPullRow(t, f.db, f.repoID, f.ownerID, 1, "open")

	missing := m.ID + 1_000_000
	tests := []struct {
		name string
		id   *int64
		want error
	}{
		{"cross-repo milestone", &foreign.ID, store.ErrMilestoneRepoMismatch},
		{"nonexistent milestone", &missing, store.ErrMilestoneNotFound},
	}
	for _, tc := range tests {
		if err := f.ms.SetPull(ctx, pullID, tc.id); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if got, _ := f.ms.GetPullID(ctx, pullID); got != nil {
			t.Errorf("%s: milestone set despite error: %d", tc.name, *got)
		}
	}

	if err := f.ms.SetPull(ctx, pullID, &m.ID); err != nil {
		t.Fatalf("SetPull: %v", err)
	}
	if got, err := f.ms.GetPullID(ctx, pullID); err != nil || got == nil || *got != m.ID {
		t.Fatalf("GetPullID = %v, %v; want %d", got, err, m.ID)
	}
	if err := f.ms.SetPull(ctx, pullID, nil); err != nil {
		t.Fatalf("SetPull(nil): %v", err)
	}
	if got, err := f.ms.GetPullID(ctx, pullID); err != nil || got != nil {
		t.Errorf("GetPullID after clear = %v, %v; want nil", got, err)
	}
}

func TestMilestoneStore_SetIssue_Clear(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "m")
	issueID := f.issue(t, 1, f.ownerID, m.ID, "open", "public")

	if got, err := f.ms.GetIssueID(ctx, issueID); err != nil || got == nil || *got != m.ID {
		t.Fatalf("GetIssueID = %v, %v; want %d", got, err, m.ID)
	}
	if err := f.ms.SetIssue(ctx, issueID, nil); err != nil {
		t.Fatalf("SetIssue(nil): %v", err)
	}
	if got, err := f.ms.GetIssueID(ctx, issueID); err != nil || got != nil {
		t.Errorf("GetIssueID after clear = %v, %v; want nil", got, err)
	}
}

func TestMilestoneStore_GetIDs_UnknownRow(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	if _, err := f.ms.GetIssueID(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetIssueID unknown: %v, want sql.ErrNoRows", err)
	}
	if _, err := f.ms.GetPullID(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetPullID unknown: %v, want sql.ErrNoRows", err)
	}
}

func TestMilestoneStore_ListIssues(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "m")
	empty := f.create(t, "empty")
	i1 := f.issue(t, 1, f.otherID, m.ID, "open", "public")
	i2 := f.issue(t, 2, f.otherID, m.ID, "open", "private")
	i3 := f.issue(t, 3, f.otherID, m.ID, "open", "public")
	i4 := f.issue(t, 4, f.otherID, m.ID, "closed", "public")
	for n, id := range map[int]int64{1: i1, 2: i2, 3: i3, 4: i4} {
		testutil.Exec(t, f.db, `UPDATE issues SET created_at = NOW() - make_interval(mins => $1) WHERE id = $2`, 10-n, id)
	}

	ids, err := f.ms.ListIssuesByMilestone(ctx, m.ID)
	if err != nil {
		t.Fatalf("ListIssuesByMilestone: %v", err)
	}
	slices.Sort(ids)
	if want := []int64{i1, i2, i3, i4}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if ids, _ := f.ms.ListIssuesByMilestone(ctx, empty.ID); len(ids) != 0 {
		t.Errorf("empty milestone ids = %v", ids)
	}

	t.Run("paging is newest first and honours visibility", func(t *testing.T) {
		page1, err := f.ms.ListIssuesPaged(ctx, m.ID, "open", nil, 1, 1)
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		page2, err := f.ms.ListIssuesPaged(ctx, m.ID, "open", nil, 2, 1)
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		page3, _ := f.ms.ListIssuesPaged(ctx, m.ID, "open", nil, 3, 1)
		if len(page1) != 1 || page1[0].Number != 3 || len(page2) != 1 || page2[0].Number != 1 || len(page3) != 0 {
			t.Errorf("anonymous pages = %v / %v / %v; want #3, #1, none", issueNumbers(page1), issueNumbers(page2), issueNumbers(page3))
		}
	})

	t.Run("author sees private, closed filter", func(t *testing.T) {
		open, err := f.ms.ListIssuesPaged(ctx, m.ID, "open", &f.otherID, 1, 10)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if want := []int{3, 2, 1}; !slices.Equal(issueNumbers(open), want) {
			t.Errorf("open = %v, want %v", issueNumbers(open), want)
		}
		closed, err := f.ms.ListIssuesPaged(ctx, m.ID, "closed", nil, 1, 10)
		if err != nil {
			t.Fatalf("closed: %v", err)
		}
		if want := []int{4}; !slices.Equal(issueNumbers(closed), want) {
			t.Errorf("closed = %v, want %v", issueNumbers(closed), want)
		}
	})
}

func TestMilestoneStore_PullCounts(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "m")
	f.pull(t, 1, "open", m.ID)
	f.pull(t, 2, "closed", m.ID)
	f.pull(t, 3, "merged", m.ID)
	f.pull(t, 4, "open", m.ID)
	seedPullRow(t, f.db, f.repoID, f.ownerID, 5, "open")

	open, closed, err := f.ms.PullCounts(ctx, m.ID)
	if err != nil {
		t.Fatalf("PullCounts: %v", err)
	}
	if open != 2 || closed != 2 {
		t.Errorf("open/closed = %d/%d, want 2/2 (merged counts as closed, unattached PR excluded)", open, closed)
	}
	if open, closed, err := f.ms.PullCounts(ctx, m.ID+1_000_000); err != nil || open != 0 || closed != 0 {
		t.Errorf("unknown milestone = %d/%d, %v; want 0/0", open, closed, err)
	}
}

func TestMilestoneStore_ListPullsPaged(t *testing.T) {
	f := newMilestoneFixture(t)
	ctx := context.Background()
	m := f.create(t, "m")
	f.pull(t, 1, "open", m.ID)
	f.pull(t, 2, "closed", m.ID)
	f.pull(t, 3, "merged", m.ID)
	withSHA := f.pull(t, 4, "open", m.ID)
	seedPullRow(t, f.db, f.repoID, f.ownerID, 5, "open")
	testutil.Exec(t, f.db, `UPDATE pull_requests SET head_sha = 'abc123' WHERE id = $1`, withSHA)

	numbers := func(prs []model.PullRequest) []int {
		out := make([]int, len(prs))
		for i, p := range prs {
			out[i] = p.Number
		}
		return out
	}
	tests := []struct {
		name           string
		state          string
		page, pageSize int
		want           []int
	}{
		{"open", "open", 1, 10, []int{4, 1}},
		{"closed includes merged", "closed", 1, 10, []int{3, 2}},
		{"closed page 2", "closed", 2, 1, []int{2}},
		{"past the end", "open", 3, 2, []int{}},
	}
	for _, tc := range tests {
		got, err := f.ms.ListPullsPaged(ctx, m.ID, tc.state, tc.page, tc.pageSize)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.Equal(numbers(got), tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, numbers(got), tc.want)
		}
	}

	got, err := f.ms.ListPullsPaged(ctx, m.ID, "open", 1, 10)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got[0].HeadSHA != "abc123" || got[1].HeadSHA != "" {
		t.Errorf("HeadSHA = %q, %q; want abc123, empty", got[0].HeadSHA, got[1].HeadSHA)
	}
}
