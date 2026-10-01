package handler_test

// Integration tests: lists that span owners show a viewer only the repos
// they can read. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type repoListFixture struct {
	public, private            seededRepo
	orgPrivateID               int64
	orgPrivateName             string
	orgOwner, reader, stranger signedInUser
}

func seedRepoListFixture(t *testing.T, db *sql.DB) repoListFixture {
	t.Helper()
	f := repoListFixture{
		public:         seedOwnedRepo(t, db, false),
		private:        seedOwnedRepo(t, db, true),
		orgPrivateName: "orgsecret_" + testutil.UniqueSuffix(t),
		orgOwner:       seedSignedInUser(t, db),
		reader:         seedSignedInUser(t, db),
		stranger:       seedSignedInUser(t, db),
	}
	testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'reader')`, f.reader.id, f.private.id)

	orgName := "testorg_" + testutil.UniqueSuffix(t)
	seedOrgOwnedBy(t, db, orgName, f.orgOwner.id)
	if err := db.QueryRow(
		`INSERT INTO repositories (owner_name, org_id, created_by, name, private)
		 SELECT $1, id, $2, $3, TRUE FROM organizations WHERE name = $1 RETURNING id`,
		orgName, f.orgOwner.id, f.orgPrivateName,
	).Scan(&f.orgPrivateID); err != nil {
		t.Fatalf("seed private org repo: %v", err)
	}
	return f
}

func TestListRepos_ListsOnlyReposTheViewerCanRead(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	f := seedRepoListFixture(t, db)
	repoIDs := map[string]int64{"public": f.public.id, "private": f.private.id, "org private": f.orgPrivateID}

	for _, tc := range []struct {
		viewer, token string
		readable      []string
	}{
		{"anonymous", "", []string{"public"}},
		{"signed-in non-member", f.stranger.token, []string{"public"}},
		{"reader", f.reader.token, []string{"public", "private"}},
		{"repo owner", f.private.owner.token, []string{"public", "private"}},
		{"org owner", f.orgOwner.token, []string{"public", "org private"}},
	} {
		t.Run(tc.viewer, func(t *testing.T) {
			rr := requestAPI(api, http.MethodGet, "/api/repos", tc.token)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d %s", rr.Code, rr.Body.String())
			}
			var repos []model.Repository
			if err := json.Unmarshal(rr.Body.Bytes(), &repos); err != nil {
				t.Fatalf("decode repos: %v", err)
			}
			for label, id := range repoIDs {
				listed := slices.ContainsFunc(repos, func(r model.Repository) bool { return r.ID == id })
				if want := slices.Contains(tc.readable, label); listed != want {
					t.Errorf("%s repo listed = %v, want %v", label, listed, want)
				}
			}
		})
	}
}

func TestPageHome_Anonymous_ListsOnlyPublicRepos(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	f := seedRepoListFixture(t, db)

	rr := httptest.NewRecorder()
	api.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	assertContains(t, body, f.public.name)
	for label, name := range map[string]string{"private": f.private.name, "org private": f.orgPrivateName} {
		if strings.Contains(body, name) {
			t.Errorf("anonymous home page lists the %s repo %q", label, name)
		}
	}
}
