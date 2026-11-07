package store_test

// Integration test for IssueStore.ListFiltered and PullStore.ListFiltered;
// skipped when TEST_DATABASE_DSN is unset.

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type listFixture struct {
	db        *sql.DB
	repoID    int64
	alice     string
	bob       string
	aliceID   int64
	bobID     int64
	labelID   map[string]int64
	milestone int64
}

func seedListFixture(t *testing.T, ctx context.Context) listFixture {
	t.Helper()
	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "lstf")
	t.Cleanup(cleanup)
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	f := listFixture{db: db, aliceID: aliceID, bobID: bobID, labelID: map[string]int64{}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, aliceID).Scan(&f.alice))
	must(db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, bobID).Scan(&f.bob))
	f.repoID = insertRepo(t, ctx, db, aliceID, f.alice, "lf_"+testutil.UniqueSuffix(t))
	for _, name := range []string{"bug", "ui"} {
		var id int64
		must(db.QueryRowContext(ctx,
			`INSERT INTO labels (repo_id, name, color, description) VALUES ($1, $2, '#ff0000', '') RETURNING id`,
			f.repoID, name).Scan(&id))
		f.labelID[name] = id
	}
	must(db.QueryRowContext(ctx,
		`INSERT INTO milestones (repo_id, number, title) VALUES ($1, 1, 'v1') RETURNING id`, f.repoID).Scan(&f.milestone))
	return f
}

func (f listFixture) exec(t *testing.T, ctx context.Context, q string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestIssueStore_ListFiltered(t *testing.T) {
	ctx := context.Background()
	f := seedListFixture(t, ctx)
	issue := func(number int, author int64, title, state string, milestone any) int64 {
		var id int64
		if err := f.db.QueryRowContext(ctx,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state, milestone_id)
			 VALUES ($1, $2, $3, $4, '', $5, $6) RETURNING id`,
			f.repoID, number, author, title, state, milestone).Scan(&id); err != nil {
			t.Fatalf("insert issue %d: %v", number, err)
		}
		return id
	}
	i1 := issue(1, f.aliceID, "Fix crash on login", "open", f.milestone)
	i2 := issue(2, f.bobID, "Docs typo", "open", nil)
	issue(3, f.aliceID, "Discount is 100% off", "closed", nil)
	i4 := issue(4, f.aliceID, "Login page slow", "open", nil)
	f.exec(t, ctx, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2), ($1, $3), ($4, $2)`,
		i1, f.labelID["bug"], f.labelID["ui"], i2)
	f.exec(t, ctx, `INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`, i1, f.bobID)
	f.exec(t, ctx, `INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, 'a'), ($1, $2, $3, 'b')`,
		f.repoID, i4, f.aliceID)

	s := store.NewIssueStore(f.db)
	numbers := func(fl store.ListFilter) ([]int, int) {
		t.Helper()
		got, total, err := s.ListFiltered(ctx, f.repoID, &f.aliceID, fl)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, iss := range got {
			out = append(out, iss.Number)
		}
		return out, total
	}

	tests := []struct {
		name string
		fl   store.ListFilter
		want []int
	}{
		{"open newest first", store.ListFilter{State: "open"}, []int{4, 2, 1}},
		{"closed", store.ListFilter{State: "closed"}, []int{3}},
		{"all states", store.ListFilter{}, []int{4, 3, 2, 1}},
		{"oldest", store.ListFilter{State: "open", Sort: "oldest"}, []int{1, 2, 4}},
		{"most commented", store.ListFilter{State: "open", Sort: "comments"}, []int{4, 2, 1}},
		{"text is case-insensitive", store.ListFilter{State: "open", Text: "LOGIN"}, []int{4, 1}},
		{"percent in text is literal", store.ListFilter{Text: "100%"}, []int{3}},
		{"author", store.ListFilter{State: "open", Author: f.bob}, []int{2}},
		{"assignee", store.ListFilter{Assignee: f.bob}, []int{1}},
		{"one label", store.ListFilter{State: "open", Labels: []string{"BUG"}}, []int{2, 1}},
		{"labels AND", store.ListFilter{State: "open", Labels: []string{"bug", "ui"}}, []int{1}},
		{"milestone title", store.ListFilter{Milestone: "V1"}, []int{1}},
		{"milestone id", store.ListFilter{MilestoneID: f.milestone}, []int{1}},
		{"no label", store.ListFilter{State: "open", No: []string{"label"}}, []int{4}},
		{"no milestone", store.ListFilter{State: "open", No: []string{"milestone"}}, []int{4, 2}},
		{"no assignee", store.ListFilter{State: "open", No: []string{"assignee"}}, []int{4, 2}},
		{"no match", store.ListFilter{NoMatch: true}, nil},
		{"combined", store.ListFilter{State: "open", Text: "login", Labels: []string{"bug"}, Author: f.alice}, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total := numbers(tt.fl)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("numbers = %v, want %v", got, tt.want)
			}
			if total != len(tt.want) {
				t.Errorf("total = %d, want %d", total, len(tt.want))
			}
		})
	}

	t.Run("paging keeps the full total", func(t *testing.T) {
		got, total := numbers(store.ListFilter{State: "open", Limit: 2, Offset: 2})
		if !reflect.DeepEqual(got, []int{1}) || total != 3 {
			t.Errorf("got %v total %d, want [1] total 3", got, total)
		}
	})

	t.Run("count by state ignores filters", func(t *testing.T) {
		open, closed, err := s.CountByState(ctx, f.repoID, &f.aliceID)
		if err != nil || open != 3 || closed != 1 {
			t.Errorf("open=%d closed=%d err=%v, want 3, 1", open, closed, err)
		}
	})
}

func TestPullStore_ListFiltered(t *testing.T) {
	ctx := context.Background()
	f := seedListFixture(t, ctx)
	pull := func(number int, author int64, title, state string, draft bool, milestone any) int64 {
		var id int64
		if err := f.db.QueryRowContext(ctx,
			`INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch, is_draft, milestone_id)
			 VALUES ($1, $2, $3, $4, '', $5, 'feat', 'main', $6, $7) RETURNING id`,
			f.repoID, number, author, title, state, draft, milestone).Scan(&id); err != nil {
			t.Fatalf("insert pull %d: %v", number, err)
		}
		return id
	}
	p1 := pull(1, f.aliceID, "Add login", "open", false, f.milestone)
	pull(2, f.bobID, "WIP search", "open", true, nil)
	pull(3, f.aliceID, "Old fix", "merged", false, nil)
	pull(4, f.bobID, "Rejected", "closed", false, nil)
	f.exec(t, ctx, `INSERT INTO pull_labels (pull_id, label_id) VALUES ($1, $2)`, p1, f.labelID["bug"])
	f.exec(t, ctx, `INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`, p1, f.bobID)

	s := store.NewPullStore(f.db)
	numbers := func(fl store.ListFilter) ([]int, int) {
		t.Helper()
		got, total, err := s.ListFiltered(ctx, f.repoID, fl)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, p := range got {
			out = append(out, p.Number)
		}
		return out, total
	}
	tests := []struct {
		name string
		fl   store.ListFilter
		want []int
	}{
		{"open excludes drafts", store.ListFilter{State: "open"}, []int{1}},
		{"draft", store.ListFilter{State: "draft"}, []int{2}},
		{"merged", store.ListFilter{State: "merged"}, []int{3}},
		{"closed", store.ListFilter{State: "closed"}, []int{4}},
		{"all", store.ListFilter{}, []int{4, 3, 2, 1}},
		{"oldest", store.ListFilter{Sort: "oldest"}, []int{1, 2, 3, 4}},
		{"author", store.ListFilter{Author: f.bob}, []int{4, 2}},
		{"assignee", store.ListFilter{Assignee: f.bob}, []int{1}},
		{"label", store.ListFilter{Labels: []string{"bug"}}, []int{1}},
		{"milestone", store.ListFilter{Milestone: "v1"}, []int{1}},
		{"no label", store.ListFilter{No: []string{"label"}}, []int{4, 3, 2}},
		{"text", store.ListFilter{Text: "search"}, []int{2}},
		{"no match", store.ListFilter{NoMatch: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total := numbers(tt.fl)
			if !reflect.DeepEqual(got, tt.want) || total != len(tt.want) {
				t.Errorf("got %v total %d, want %v", got, total, tt.want)
			}
		})
	}

	t.Run("paging keeps the full total", func(t *testing.T) {
		got, total := numbers(store.ListFilter{Limit: 2, Offset: 2})
		if !reflect.DeepEqual(got, []int{2, 1}) || total != 4 {
			t.Errorf("got %v total %d, want [2 1] total 4", got, total)
		}
	})

	t.Run("count by state", func(t *testing.T) {
		c, err := s.CountByState(ctx, f.repoID)
		if err != nil || c != (store.PullStateCounts{Open: 1, Draft: 1, Merged: 1, Closed: 1}) {
			t.Errorf("counts = %+v err=%v", c, err)
		}
	})
}

func TestPullStore_ReviewFilterAndStates(t *testing.T) {
	ctx := context.Background()
	f := seedListFixture(t, ctx)
	pull := func(number int, base string) int64 {
		var id int64
		if err := f.db.QueryRowContext(ctx,
			`INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch)
			 VALUES ($1, $2, $3, 'pr', '', 'open', 'feat', $4) RETURNING id`,
			f.repoID, number, f.aliceID, base).Scan(&id); err != nil {
			t.Fatalf("insert pull %d: %v", number, err)
		}
		return id
	}
	review := func(pullID, author int64, name, state string) {
		f.exec(t, ctx, `INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body) VALUES ($1, $2, $3, $4, $5, '')`,
			pullID, f.repoID, author, name, state)
	}
	p1 := pull(1, "main")
	pull(2, "main")
	p3 := pull(3, "release")
	p4 := pull(4, "main")
	p5 := pull(5, "release")
	review(p1, f.bobID, f.bob, "approved")
	review(p3, f.bobID, f.bob, "approved")
	review(p4, f.bobID, f.bob, "changes_requested")
	review(p4, f.aliceID, f.alice, "approved")
	review(p5, f.bobID, f.bob, "approved")
	review(p5, f.aliceID, f.alice, "approved")
	review(p1, f.aliceID, f.alice, "commented")

	s := store.NewPullStore(f.db)
	required := map[string]int{"release": 2}
	byReview := func(state string) []int {
		t.Helper()
		got, total, err := s.ListFiltered(ctx, f.repoID, store.ListFilter{Review: state, ReviewRequired: required, Sort: "oldest"})
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, p := range got {
			out = append(out, p.Number)
		}
		if total != len(out) {
			t.Errorf("review:%s total = %d for %d rows", state, total, len(out))
		}
		return out
	}
	for state, want := range map[string][]int{
		"approved":          {1, 5},
		"none":              {2},
		"required":          {3},
		"changes_requested": {4},
	} {
		if got := byReview(state); !reflect.DeepEqual(got, want) {
			t.Errorf("review:%s = %v, want %v", state, got, want)
		}
	}

	states, err := s.ReviewStates(ctx, []int64{p1, p3, p4, p5}, required)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]string{p1: "approved", p3: "required", p4: "changes_requested", p5: "approved"}; !reflect.DeepEqual(states, want) {
		t.Errorf("ReviewStates = %v, want %v", states, want)
	}

	// Without a protection rule the same release PR with one approval reads as approved.
	got, _, _ := s.ListFiltered(ctx, f.repoID, store.ListFilter{Review: "approved", Sort: "oldest"})
	var nums []int
	for _, p := range got {
		nums = append(nums, p.Number)
	}
	if want := []int{1, 3, 5}; !reflect.DeepEqual(nums, want) {
		t.Errorf("no rule: review:approved = %v, want %v", nums, want)
	}

	branches, err := s.BaseBranches(ctx, f.repoID)
	if err != nil || len(branches) != 2 {
		t.Errorf("BaseBranches = %v, %v", branches, err)
	}
}
