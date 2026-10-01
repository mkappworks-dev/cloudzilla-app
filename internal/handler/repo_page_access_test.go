package handler_test

// Integration tests: signed-in repo pages must not reveal a private repo to a
// user who cannot read it. All tests require TEST_DATABASE_DSN and skip
// otherwise.

import (
	"net/http"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type repoPageCase struct{ method, page string }

var newIssueAndPullPageCases = []repoPageCase{
	{http.MethodGet, "/issues/new"},
	{http.MethodPost, "/issues/new"},
	{http.MethodGet, "/pulls/new"},
	{http.MethodPost, "/pulls/new"},
}

// Pages that need more than read access.
var gatedRepoPageCases = []repoPageCase{
	{http.MethodGet, "/discussions/new"},
	{http.MethodPost, "/discussions/new"},
	{http.MethodGet, "/milestones/new"},
	{http.MethodPost, "/milestones/new"},
	{http.MethodPost, "/milestones/1"},
	{http.MethodGet, "/releases/new"},
	{http.MethodGet, "/new/main"},
	{http.MethodPost, "/new/main"},
	{http.MethodGet, "/settings"},
	{http.MethodPost, "/settings/general"},
	{http.MethodPost, "/settings/features"},
	{http.MethodPost, "/settings/visibility"},
	{http.MethodGet, "/wiki/new"},
	{http.MethodGet, "/wiki/Home/edit"},
}

// A 403 for a private repo against a 404 for a missing one would confirm that
// the private repo exists, so the two responses must be identical.
func TestRepoPages_PrivateRepoNonReader_LooksLikeMissingRepo(t *testing.T) {
	for _, tc := range append(newIssueAndPullPageCases, gatedRepoPageCases...) {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedOwnedRepo(t, db, true)
			token := seedSignedInUser(t, db).token
			missingPath := "/nobody_" + testutil.UniqueSuffix(t) + "/norepo"

			private := requestPage(t, db, tc.method, repo.path+tc.page, token)
			missing := requestPage(t, db, tc.method, missingPath+tc.page, token)

			if private.Code != http.StatusNotFound {
				t.Errorf("want 404, got %d", private.Code)
			}
			if private.Code != missing.Code || private.Body.String() != missing.Body.String() {
				t.Errorf("private repo response (%d) must match a missing repo's (%d)", private.Code, missing.Code)
			}
		})
	}
}

func TestNewIssueAndPullPages_PrivateRepoOwner_RendersForm(t *testing.T) {
	for _, tc := range newIssueAndPullPageCases {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedOwnedRepo(t, db, true)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, repo.owner.token)

			if rr.Code != http.StatusOK {
				t.Errorf("want 200, got %d", rr.Code)
			}
			assertContains(t, rr.Body.String(), repo.name)
		})
	}
}

func TestGatedRepoPages_PublicRepoNonWriter_Forbidden(t *testing.T) {
	for _, tc := range gatedRepoPageCases {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedOwnedRepo(t, db, false)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, seedSignedInUser(t, db).token)

			if rr.Code != http.StatusForbidden {
				t.Errorf("want 403, got %d", rr.Code)
			}
		})
	}
}
