package store_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// Search must not show a viewer anything the page it links to would 404 on.
// The search word sits only in bodies and file contents, so a hit on a hidden
// row would also confirm what that row says.
func TestSearch_HidesWhatTheViewerCannotRead(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	word := "srchvis" + strings.ReplaceAll(suffix, "_", "x")

	ownerName := "srchowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	writerID := testutil.SeedUser(t, db, "srchwriter_"+suffix)
	readerID := testutil.SeedUser(t, db, "srchreader_"+suffix)
	strangerID := testutil.SeedUser(t, db, "srchstranger_"+suffix)

	mkRepo := func(name string, private, deleted bool) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, name, private, deleted_at, deleted_by)
			 VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN NOW() END, CASE WHEN $5 THEN $1::bigint END)
			 RETURNING id`,
			ownerID, ownerName, name+"_"+suffix, private, deleted,
		).Scan(&id); err != nil {
			t.Fatalf("insert repo %s: %v", name, err)
		}
		return id
	}
	public := mkRepo("pub", false, false)
	private := mkRepo("priv", true, false)
	deleted := mkRepo("binned", false, true)
	testutil.Exec(t, db,
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer'), ($3, $2, 'reader'), ($3, $4, 'reader')`,
		writerID, public, readerID, private)

	mkIssue := func(repoID int64, number int, title, visibility string) {
		t.Helper()
		testutil.Exec(t, db,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, $2, $3, $4, $5, 'open', $6)`,
			repoID, number, ownerID, title, word, visibility)
	}
	mkIssue(public, 1, "public issue", "public")
	mkIssue(public, 2, "private issue", "private")
	mkIssue(private, 1, "issue in private repo", "public")
	mkIssue(deleted, 1, "issue in deleted repo", "public")

	mkPull := func(repoID int64, title string) {
		t.Helper()
		testutil.Exec(t, db,
			`INSERT INTO pull_requests (repo_id, number, author_id, title, body, head_branch, base_branch) VALUES ($1, 100, $2, $3, $4, 'feature', 'main')`,
			repoID, ownerID, title, word)
	}
	mkPull(public, "public pull")
	mkPull(private, "pull in private repo")
	mkPull(deleted, "pull in deleted repo")

	codeIndex := store.NewCodeSearchStore(db)
	for path, repoID := range map[string]int64{"public.go": public, "private.go": private, "deleted.go": deleted} {
		if err := codeIndex.Index(ctx, repoID, "main", path, word); err != nil {
			t.Fatalf("index %s: %v", path, err)
		}
	}

	viewers := []struct {
		name string
		id   *int64
	}{
		{"anonymous", nil},
		{"stranger", &strangerID},
		{"reader", &readerID},
		{"writer", &writerID},
		{"owner", &ownerID},
	}
	all := []string{"anonymous", "stranger", "reader", "writer", "owner"}
	// check runs search as every viewer; want maps each result to who may see it.
	check := func(t *testing.T, want map[string][]string, search func(viewer *int64) ([]string, error)) {
		t.Helper()
		for _, v := range viewers {
			got, err := search(v.id)
			if err != nil {
				t.Fatalf("as %s: %v", v.name, err)
			}
			var exp []string
			for result, who := range want {
				if slices.Contains(who, v.name) {
					exp = append(exp, result)
				}
			}
			slices.Sort(got)
			slices.Sort(exp)
			if !slices.Equal(got, exp) {
				t.Errorf("as %s: got %q, want %q", v.name, got, exp)
			}
		}
	}
	search := store.NewSearchStore(db)

	t.Run("issues", func(t *testing.T) {
		check(t, map[string][]string{
			"public issue":          all,
			"private issue":         {"writer", "owner"},
			"issue in private repo": {"reader", "owner"},
			"issue in deleted repo": nil,
		}, func(viewer *int64) ([]string, error) {
			issues, err := search.SearchIssues(ctx, word, viewer, 100)
			titles := make([]string, len(issues))
			for i, iss := range issues {
				titles[i] = iss.Title
			}
			return titles, err
		})
	})

	t.Run("pulls", func(t *testing.T) {
		check(t, map[string][]string{
			"public pull":          all,
			"pull in private repo": {"reader", "owner"},
			"pull in deleted repo": nil,
		}, func(viewer *int64) ([]string, error) {
			pulls, err := search.SearchPulls(ctx, word, viewer, 100)
			titles := make([]string, len(pulls))
			for i, p := range pulls {
				titles[i] = p.Title
			}
			return titles, err
		})
	})

	// Code search has no viewer: it only ever covers public repos.
	t.Run("code", func(t *testing.T) {
		results, total, err := codeIndex.Search(ctx, word, nil, "", 1, 100)
		if err != nil {
			t.Fatalf("code search: %v", err)
		}
		var paths []string
		for _, r := range results {
			paths = append(paths, r.FilePath)
		}
		if !slices.Equal(paths, []string{"public.go"}) || total != 1 {
			t.Errorf("got %q (total %d), want only public.go", paths, total)
		}
	})
}
