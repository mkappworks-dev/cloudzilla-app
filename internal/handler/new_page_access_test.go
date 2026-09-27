package handler_test

// Integration tests: new issue/PR pages must not render a private repo to a
// signed-in user who cannot read it. All tests require TEST_DATABASE_DSN and
// skip otherwise.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var newPageCases = []struct{ method, page string }{
	{http.MethodGet, "/issues/new"},
	{http.MethodPost, "/issues/new"},
	{http.MethodGet, "/pulls/new"},
	{http.MethodPost, "/pulls/new"},
}

func TestNewIssueAndPullPages_PrivateRepoNonReader_NotFound(t *testing.T) {
	for _, tc := range newPageCases {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedOwnedRepo(t, db, true)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, seedSignedInUser(t, db).token)

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
			repo := seedOwnedRepo(t, db, true)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, repo.owner.token)

			if rr.Code != http.StatusOK {
				t.Errorf("want 200, got %d", rr.Code)
			}
			assertContains(t, rr.Body.String(), repo.name)
		})
	}
}

var writeGatedNewPageCases = []struct{ method, page string }{
	{http.MethodGet, "/discussions/new"},
	{http.MethodPost, "/discussions/new"},
	{http.MethodGet, "/milestones/new"},
	{http.MethodPost, "/milestones/new"},
}

// These pages need write access, but a 403 for a private repo against a 404
// for a missing one would confirm that the private repo exists.
func TestWriteGatedNewPages_PrivateRepoNonReader_LooksLikeMissingRepo(t *testing.T) {
	for _, tc := range writeGatedNewPageCases {
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

func TestWriteGatedNewPages_PublicRepoNonWriter_Forbidden(t *testing.T) {
	for _, tc := range writeGatedNewPageCases {
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
