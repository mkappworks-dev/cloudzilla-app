package store_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A search box is typed into: the results page has to answer for half a word
// the way the suggestions dropdown does.
func TestSearch_MatchesWordPrefixes(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	word := "pfx" + strings.ReplaceAll(suffix, "_", "x")

	ownerName := "pfxowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	strangerID := testutil.SeedUser(t, db, "pfxstranger_"+suffix)
	t.Cleanup(func() { testutil.DeleteUsers(t, db, ownerID, strangerID) })

	mkRepo := func(name, description string, private bool) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, name, description, private) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			ownerID, "testuser_"+ownerName, name, description, private,
		).Scan(&id); err != nil {
			t.Fatalf("insert repo %s: %v", name, err)
		}
		return id
	}
	byName := mkRepo(word+"vault", "", false)
	byDescription := mkRepo("plain_"+suffix, "a "+word+"keeper for secrets", false)
	twoWords := mkRepo("twowords_"+suffix, "alpha"+word+" beta"+word, false)
	private := mkRepo(word+"hidden", "", true)

	testutil.Exec(t, db,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 1, $2, $3, $4, 'open', 'public')`,
		byName, ownerID, word+"title issue", "nothing")
	testutil.Exec(t, db,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 2, $2, 'by body', $3, 'open', 'public')`,
		byName, ownerID, "see the "+word+"body here")
	testutil.Exec(t, db,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, head_branch, base_branch) VALUES ($1, 100, $2, $3, 'nothing', 'feature', 'main')`,
		byName, ownerID, word+"pull request")

	search := store.NewSearchStore(db)
	repoIDs := func(t *testing.T, q string, viewer *int64) []int64 {
		t.Helper()
		repos, err := search.SearchRepos(ctx, q, viewer, 100)
		if err != nil {
			t.Fatalf("SearchRepos(%q): %v", q, err)
		}
		ids := make([]int64, len(repos))
		for i, r := range repos {
			ids[i] = r.ID
		}
		slices.Sort(ids)
		return ids
	}
	wantIDs := func(ids ...int64) []int64 { slices.Sort(ids); return ids }

	t.Run("repos by name and description prefix", func(t *testing.T) {
		got := repoIDs(t, word[:len(word)-1], &ownerID)
		if want := wantIDs(byName, byDescription, private); !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("the whole word still matches", func(t *testing.T) {
		if got := repoIDs(t, word+"vault", &ownerID); !slices.Equal(got, []int64{byName}) {
			t.Errorf("got %v, want [%d]", got, byName)
		}
	})

	t.Run("every word of the query is a prefix", func(t *testing.T) {
		if got := repoIDs(t, "alpha"+word[:6]+" beta"+word[:6], &ownerID); !slices.Equal(got, []int64{twoWords}) {
			t.Errorf("got %v, want [%d]", got, twoWords)
		}
		if got := repoIDs(t, "alpha"+word[:6]+" zzzzqq", &ownerID); len(got) != 0 {
			t.Errorf("a word that matches nothing should empty the result, got %v", got)
		}
	})

	t.Run("a private repo stays hidden", func(t *testing.T) {
		for name, viewer := range map[string]*int64{"anonymous": nil, "stranger": &strangerID} {
			if got := repoIDs(t, word[:len(word)-1], viewer); slices.Contains(got, private) {
				t.Errorf("%s was shown the private repo", name)
			}
		}
	})

	t.Run("issues and pulls", func(t *testing.T) {
		issues, err := search.SearchIssues(ctx, word[:len(word)-1], &ownerID, 100)
		if err != nil {
			t.Fatalf("SearchIssues: %v", err)
		}
		titles := make([]string, len(issues))
		for i, iss := range issues {
			titles[i] = iss.Title
		}
		slices.Sort(titles)
		if want := []string{"by body", word + "title issue"}; !slices.Equal(titles, want) {
			t.Errorf("issues: got %q, want %q", titles, want)
		}

		pulls, err := search.SearchPulls(ctx, word[:len(word)-1], &ownerID, 100)
		if err != nil || len(pulls) != 1 || pulls[0].Title != word+"pull request" {
			t.Errorf("pulls: got %+v, err %v", pulls, err)
		}
	})

	t.Run("query syntax is data, not tsquery", func(t *testing.T) {
		for _, q := range []string{"& | ! ( ) : * '", "-", "   ", `\`, "foo' & (bar", "!!", "the", "a for the"} {
			if got := repoIDs(t, q, &ownerID); len(got) != 0 {
				t.Errorf("SearchRepos(%q) = %v, want none", q, got)
			}
			if issues, err := search.SearchIssues(ctx, q, &ownerID, 100); err != nil || len(issues) != 0 {
				t.Errorf("SearchIssues(%q) = %d rows, err %v", q, len(issues), err)
			}
			if pulls, err := search.SearchPulls(ctx, q, &ownerID, 100); err != nil || len(pulls) != 0 {
				t.Errorf("SearchPulls(%q) = %d rows, err %v", q, len(pulls), err)
			}
		}
	})
}
