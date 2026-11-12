package store_test

// Integration test for IssueStore.ListInvolving and PullStore.ListInvolving;
// skipped when TEST_DATABASE_DSN is unset.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestIssueStore_ListInvolving(t *testing.T) {
	ctx := context.Background()
	f := seedListFixture(t, ctx)
	bobRepoName := "bobs_" + testutil.UniqueSuffix(t)
	bobRepo := insertRepo(t, ctx, f.db, f.bobID, f.bob, bobRepoName)
	issue := func(repoID int64, number int, author int64, title, state string) int64 {
		var id int64
		if err := f.db.QueryRowContext(ctx,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state) VALUES ($1, $2, $3, $4, '', $5) RETURNING id`,
			repoID, number, author, title, state).Scan(&id); err != nil {
			t.Fatalf("insert issue %d: %v", number, err)
		}
		return id
	}
	authored := issue(f.repoID, 1, f.aliceID, "authored", "open")
	assigned := issue(f.repoID, 2, f.bobID, "assigned", "open")
	mentioned := issue(bobRepo, 1, f.bobID, "mentioned", "open")
	closed := issue(bobRepo, 2, f.aliceID, "closed one", "closed")
	issue(bobRepo, 3, f.bobID, "unrelated", "open")
	f.exec(t, ctx, `INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`, assigned, f.aliceID)
	var commentID int64
	if err := f.db.QueryRowContext(ctx,
		`INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, 'hi') RETURNING id`,
		bobRepo, mentioned, f.bobID).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	f.exec(t, ctx, `INSERT INTO mentions (comment_id, user_id) VALUES ($1, $2)`, commentID, f.aliceID)

	s := store.NewIssueStore(f.db)
	ids := func(fl store.ListFilter) []int64 {
		t.Helper()
		got, total, err := s.ListInvolving(ctx, f.aliceID, fl)
		if err != nil {
			t.Fatal(err)
		}
		if total != len(got) {
			t.Errorf("total = %d for %d rows", total, len(got))
		}
		var out []int64
		for _, it := range got {
			out = append(out, it.ID)
		}
		return out
	}
	tests := []struct {
		name string
		fl   store.ListFilter
		want []int64
	}{
		{"open, every kind of involvement", store.ListFilter{State: "open", Sort: "oldest"}, []int64{authored, assigned, mentioned}},
		{"closed", store.ListFilter{State: "closed"}, []int64{closed}},
		{"author", store.ListFilter{State: "open", Author: f.alice}, []int64{authored}},
		{"assignee", store.ListFilter{State: "open", Assignee: f.alice}, []int64{assigned}},
		{"mentions", store.ListFilter{State: "open", Mentions: f.alice}, []int64{mentioned}},
		{"repo", store.ListFilter{State: "open", Repo: f.bob + "/" + bobRepoName}, []int64{mentioned}},
		{"repo is case-insensitive", store.ListFilter{State: "open", Repo: strings.ToUpper(f.bob + "/" + bobRepoName)}, []int64{mentioned}},
		{"repo that holds nothing of theirs", store.ListFilter{Repo: "nope/none"}, nil},
		{"no match", store.ListFilter{NoMatch: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ids(tt.fl); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("pages report the full total", func(t *testing.T) {
		got, total, err := s.ListInvolving(ctx, f.aliceID, store.ListFilter{State: "open", Limit: 1})
		if err != nil || len(got) != 1 || total != 3 {
			t.Errorf("got %d rows, total %d, err %v; want 1 row, total 3", len(got), total, err)
		}
	})
	t.Run("a stranger sees none of it", func(t *testing.T) {
		got, total, err := s.ListInvolving(ctx, f.bobID+f.aliceID+1000, store.ListFilter{})
		if err != nil || len(got) != 0 || total != 0 {
			t.Errorf("got %d rows, total %d, err %v", len(got), total, err)
		}
	})
}

func TestPullStore_ListInvolving(t *testing.T) {
	ctx := context.Background()
	f := seedListFixture(t, ctx)
	pull := func(number int, author int64, state string, draft bool) int64 {
		var id int64
		if err := f.db.QueryRowContext(ctx,
			`INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch, is_draft)
			 VALUES ($1, $2, $3, $4, '', $5, 'feat', 'main', $6) RETURNING id`,
			f.repoID, number, author, "pr", state, draft).Scan(&id); err != nil {
			t.Fatalf("insert pull %d: %v", number, err)
		}
		return id
	}
	authored := pull(1, f.aliceID, "open", false)
	draft := pull(2, f.aliceID, "open", true)
	merged := pull(3, f.aliceID, "merged", false)
	reviewing := pull(4, f.bobID, "open", false)
	assigned := pull(5, f.bobID, "open", false)
	pull(6, f.bobID, "open", false)
	f.exec(t, ctx, `INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body) VALUES ($1, $2, $3, $4, 'pending', '')`,
		reviewing, f.repoID, f.aliceID, f.alice)
	f.exec(t, ctx, `INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`, assigned, f.aliceID)

	s := store.NewPullStore(f.db)
	ids := func(fl store.ListFilter) []int64 {
		t.Helper()
		fl.Sort = "oldest"
		got, _, err := s.ListInvolving(ctx, f.aliceID, fl)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, it := range got {
			out = append(out, it.ID)
		}
		return out
	}
	tests := []struct {
		name string
		fl   store.ListFilter
		want []int64
	}{
		{"open keeps drafts", store.ListFilter{State: "open"}, []int64{authored, draft, reviewing, assigned}},
		{"draft", store.ListFilter{State: "draft"}, []int64{draft}},
		{"merged", store.ListFilter{State: "merged"}, []int64{merged}},
		{"closed excludes merged", store.ListFilter{State: "closed"}, nil},
		{"author", store.ListFilter{State: "open", Author: f.alice}, []int64{authored, draft}},
		{"assignee", store.ListFilter{State: "open", Assignee: f.alice}, []int64{assigned}},
		{"reviewer", store.ListFilter{State: "open", Reviewer: f.alice}, []int64{reviewing}},
		{"review none", store.ListFilter{State: "open", Review: "none"}, []int64{authored, reviewing, assigned}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ids(tt.fl); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
