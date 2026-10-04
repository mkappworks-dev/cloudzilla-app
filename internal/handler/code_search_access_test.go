package handler_test

// Integration tests: code search must not reveal whether a private repo exists.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// codeSearchFixture indexes a file holding word in a public and a private
// repo, so a search that ignored a repo filter would find the public one.
type codeSearchFixture struct {
	app             http.Handler
	public, private seededRepo
	word            string
}

func seedCodeSearchFixture(t *testing.T, db *sql.DB) codeSearchFixture {
	t.Helper()
	f := codeSearchFixture{
		app:     newAPIRouter(t, db),
		public:  seedOwnedRepo(t, db, false),
		private: seedOwnedRepo(t, db, true),
		word:    "needle" + strings.ReplaceAll(testutil.UniqueSuffix(t), "_", "x"),
	}
	index := store.NewCodeSearchStore(db)
	for _, repo := range []seededRepo{f.public, f.private} {
		if err := index.Index(context.Background(), repo.id, "main", repo.name+".go", "package x // "+f.word); err != nil {
			t.Fatalf("index %s: %v", repo.path, err)
		}
	}
	return f
}

func (f codeSearchFixture) search(repoFilter, token string) string {
	path := "/search/code?q=" + url.QueryEscape(f.word) + "&repo=" + url.QueryEscape(repoFilter)
	return pageResponse(requestAPI(f.app, http.MethodGet, path, token), repoFilter)
}

func TestCodeSearch_PrivateRepoFilter_LooksLikeMissingRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := seedCodeSearchFixture(t, db)
	privateFilter := strings.TrimPrefix(f.private.path, "/")
	missingFilter := "nobody_" + testutil.UniqueSuffix(t) + "/norepo"

	for _, v := range []struct{ name, token string }{
		{"anonymous", ""},
		{"stranger", seedSignedInUser(t, db).token},
	} {
		t.Run(v.name, func(t *testing.T) {
			private := f.search(privateFilter, v.token)
			missing := f.search(missingFilter, v.token)

			if strings.Contains(missing, f.public.name+".go") {
				t.Errorf("a filter naming a missing repo must match nothing, but it found the public repo's file")
			}
			if private != missing {
				t.Errorf("a private repo filter must get the same response as a missing repo filter")
			}
		})
	}
}

// Proves the fixture is indexed, so the test above compares real searches.
func TestCodeSearch_PublicRepoFilter_FindsItsFiles(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := seedCodeSearchFixture(t, db)

	got := f.search(strings.TrimPrefix(f.public.path, "/"), "")

	assertContains(t, got, f.public.name+".go")
}
