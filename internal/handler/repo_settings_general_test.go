package handler_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestUpdateRepoGeneral_InvalidDefaultBranch_Unprocessable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	rr := postForm(t, pageRouter(newPageHandler(t, db)), repo.owner.token, repo.path+"/settings/general",
		url.Values{"description": {"after"}, "default_branch": {"bad name"}})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rr.Code, rr.Body.String())
	}
	assertContains(t, rr.Body.String(), `"bad name" is not a valid branch name`)
}
