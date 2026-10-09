package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSearchOrgs(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	prefix := "srch" + strings.ReplaceAll(suffix, "_", "x")

	insert := func(name, display string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO organizations (name, display_name) VALUES ($1, $2) RETURNING id`, name, display,
		).Scan(&id); err != nil {
			t.Fatalf("insert org %s: %v", name, err)
		}
		t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, id) })
		return id
	}
	byName := insert(prefix+"-software", "Plain")
	byDisplay := insert("other_"+suffix, "Zebra "+prefix)
	insert("unrelated_"+suffix, "Nothing")

	search := store.NewSearchStore(db)
	got, err := search.SearchOrgs(ctx, prefix[:len(prefix)-1], 20)
	if err != nil {
		t.Fatalf("SearchOrgs: %v", err)
	}
	if len(got) != 1 || got[0].ID != byName {
		t.Errorf("by name prefix: got %+v, want only org %d", got, byName)
	}

	got, err = search.SearchOrgs(ctx, "zebra "+prefix, 20)
	if err != nil || len(got) != 1 || got[0].ID != byDisplay {
		t.Errorf("by display name: got %+v, err %v, want only org %d", got, err, byDisplay)
	}

	for _, q := range []string{"%", "_", prefix[:3] + "%"} {
		if got, err := search.SearchOrgs(ctx, q, 20); err != nil || len(got) != 0 {
			t.Errorf("q=%q: got %d orgs, err %v; want none", q, len(got), err)
		}
	}
}

// A result row links to /{owner}/{repo}/issues/{n}, so the store has to say which repo it is in.
func TestSearchIssuesAndPulls_CarryTheirRepo(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	word := "ctx" + strings.ReplaceAll(suffix, "_", "x")

	ownerName := "ctxowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	t.Cleanup(func() { testutil.DeleteUsers(t, db, ownerID) })

	mkRepo := func(name string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, $2, $3) RETURNING id`,
			ownerID, ownerName, name,
		).Scan(&id); err != nil {
			t.Fatalf("insert repo %s: %v", name, err)
		}
		return id
	}
	repoA, repoB := mkRepo("alpha_"+suffix), mkRepo("beta_"+suffix)
	testutil.Exec(t, db,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 1, $2, $3, '', 'open', 'public')`,
		repoA, ownerID, word+" in alpha")
	testutil.Exec(t, db,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, head_branch, base_branch) VALUES ($1, 7, $2, $3, '', 'feature', 'main')`,
		repoB, ownerID, word+" in beta")

	search := store.NewSearchStore(db)
	issues, err := search.SearchIssues(ctx, word, &ownerID, 20)
	if err != nil || len(issues) != 1 {
		t.Fatalf("SearchIssues: %d rows, err %v", len(issues), err)
	}
	if got := issues[0]; got.RepoOwner != ownerName || got.RepoName != "alpha_"+suffix || got.Visibility != "public" {
		t.Errorf("issue repo = %s/%s, visibility %q", got.RepoOwner, got.RepoName, got.Visibility)
	}

	pulls, err := search.SearchPulls(ctx, word, &ownerID, 20)
	if err != nil || len(pulls) != 1 {
		t.Fatalf("SearchPulls: %d rows, err %v", len(pulls), err)
	}
	if got := pulls[0]; got.RepoOwner != ownerName || got.RepoName != "beta_"+suffix {
		t.Errorf("pull repo = %s/%s", got.RepoOwner, got.RepoName)
	}
}
