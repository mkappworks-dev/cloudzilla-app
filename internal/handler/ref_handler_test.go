package handler_test

// Integration tests: branch deletion through the API. All tests require
// TEST_DATABASE_DSN and skip otherwise.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const protectedBranchDeleteMsg = "cannot delete a branch whose protection rule blocks force pushes"

func requestDeleteBranch(api http.Handler, repo seededRepo, branch string, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/repos"+repo.path+"/branches?name="+branch, nil)
	req.Header.Set("Authorization", "Bearer "+repo.owner.token)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rr := httptest.NewRecorder()
	api.ServeHTTP(rr, req)
	return rr
}

// The HTMX case expects the JSON body too: the layout's htmx:responseError
// handler shows its "error" field as a toast.
func TestDeleteBranch_ForcePushBlocked_RefusedAndBranchKept(t *testing.T) {
	for _, tc := range []struct {
		name string
		htmx bool
	}{{"json", false}, {"htmx", true}} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)
			testutil.Exec(t, db, `INSERT INTO branch_protections (repo_id, pattern, block_force_push) VALUES ($1, 'feature', true)`, r.id)

			rr := requestDeleteBranch(api, r.seededRepo, "feature", tc.htmx)

			want := `{"error":"` + protectedBranchDeleteMsg + `"}` + "\n"
			if rr.Code != http.StatusUnprocessableEntity || rr.Body.String() != want {
				t.Errorf("want 422 %q, got %d %q", want, rr.Code, rr.Body.String())
			}
			if got := branchHash(t, r.git, "feature"); got != r.featureTip {
				t.Errorf("feature = %s, want %s", got, r.featureTip)
			}
		})
	}
}

func TestDeleteBranch_ProtectedWithoutForcePushBlock_Deletes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `INSERT INTO branch_protections (repo_id, pattern, require_review_count, block_force_push) VALUES ($1, 'feature', 1, false)`, r.id)

	rr := requestDeleteBranch(api, r.seededRepo, "feature", false)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d %q", rr.Code, rr.Body.String())
	}
	if _, err := r.git.Reference(plumbing.NewBranchReferenceName("feature"), true); !errors.Is(err, plumbing.ErrReferenceNotFound) {
		t.Errorf("feature still resolves (err = %v), want it deleted", err)
	}
}
