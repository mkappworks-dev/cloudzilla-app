package handler_test

// Integration tests: new issue/PR pages must not render a private repo to a
// signed-in user who cannot read it. All tests require TEST_DATABASE_DSN and
// skip otherwise.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type privateRepo struct {
	path, name, ownerToken string
}

func seedPrivateRepo(t *testing.T, db *sql.DB) privateRepo {
	t.Helper()
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	if _, err := db.ExecContext(ctx, `UPDATE repositories SET private = true WHERE id = $1`, repoID); err != nil {
		t.Fatalf("make repo private: %v", err)
	}
	return privateRepo{
		path:       "/" + ownerName + "/testrepo_" + suffix,
		name:       "testrepo_" + suffix,
		ownerToken: makeIssueJWT(t, ownerID, ownerName),
	}
}

func outsiderToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	return makeIssueJWT(t, testutil.SeedUser(t, db, suffix), "testuser_"+suffix)
}

func requestPage(t *testing.T, db *sql.DB, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"title": {""}, "head_branch": {"feature"}, "base_branch": {"main"}}
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	formRouter(newFormHandler(t, db)).ServeHTTP(rr, req)
	return rr
}

var newPageCases = []struct{ method, page string }{
	{http.MethodGet, "/issues/new"},
	{http.MethodPost, "/issues/new"},
	{http.MethodGet, "/pulls/new"},
	{http.MethodPost, "/pulls/new"},
}

// The POST cases submit an empty title, the validation error that re-renders
// the form with the repo's data.
func TestNewIssueAndPullPages_PrivateRepoNonReader_NotFound(t *testing.T) {
	for _, tc := range newPageCases {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedPrivateRepo(t, db)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, outsiderToken(t, db))

			if rr.Code != http.StatusNotFound {
				t.Errorf("want 404, got %d", rr.Code)
			}
			if strings.Contains(rr.Body.String(), repo.name) {
				t.Errorf("body must not contain the private repo's name %q", repo.name)
			}
		})
	}
}

func TestNewIssueAndPullPages_PrivateRepoOwner_RendersForm(t *testing.T) {
	for _, tc := range newPageCases {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedPrivateRepo(t, db)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, repo.ownerToken)

			if rr.Code != http.StatusOK {
				t.Errorf("want 200, got %d", rr.Code)
			}
			assertContains(t, rr.Body.String(), repo.name)
		})
	}
}
