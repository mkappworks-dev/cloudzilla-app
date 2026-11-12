package handler_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const renamePassword = "rename-password"

// renameRouter serves a repo whose bare dir exists, which a rename moves.
func renameRouter(t *testing.T, db *sql.DB, repo seededRepo) http.Handler {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, repo.owner.name, repo.name+".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return pageRouter(newPageHandlerAt(t, db, root))
}

// withPassword gives the repo's owner a password, which the rename's step-up asks for.
func withPassword(t *testing.T, db *sql.DB, repo seededRepo) seededRepo {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(renamePassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), repo.owner.id)
	return repo
}

func TestRenameRepoSettings_HTMX_Renamed_RedirectsToNewSettings(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := withPassword(t, db, seedOwnedRepo(t, db, false))

	rr := postHXForm(t, renameRouter(t, db, repo), repo.owner.token, repo.path+"/settings/rename",
		url.Values{"name": {"renamed"}, "password": {renamePassword}})

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if got, want := rr.Header().Get("HX-Redirect"), "/"+repo.owner.name+"/renamed/settings"; got != want {
		t.Errorf("HX-Redirect = %q, want %q", got, want)
	}
}

// The form's data-toast treats a 200 without HX-Retarget as a save.
func TestRenameRepoSettings_HTMX_Refused_RendersFormError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := withPassword(t, db, seedOwnedRepo(t, db, false))
	router := renameRouter(t, db, repo)

	for name, form := range map[string]url.Values{
		"invalid":   {"name": {"bad name"}, "password": {renamePassword}},
		"reserved":  {"name": {"x.wiki"}, "password": {renamePassword}},
		"unchanged": {"name": {repo.name}, "password": {renamePassword}},
	} {
		t.Run(name, func(t *testing.T) {
			rr := postHXForm(t, router, repo.owner.token, repo.path+"/settings/rename", form)

			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("HX-Retarget"); got != "#repo-rename-form-error" {
				t.Errorf("HX-Retarget = %q, want #repo-rename-form-error", got)
			}
			if got := rr.Header().Get("HX-Redirect"); got != "" {
				t.Errorf("HX-Redirect = %q on an error", got)
			}
		})
	}
}

func TestRenameRepoSettings_NotManager_Forbidden(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := withPassword(t, db, seedOwnedRepo(t, db, false))
	reader := seedSignedInUser(t, db)

	rr := postHXForm(t, renameRouter(t, db, repo), reader.token, repo.path+"/settings/rename",
		url.Values{"name": {"renamed"}})

	if rr.Code != http.StatusForbidden && rr.Code != http.StatusNotFound {
		t.Fatalf("want 403 or 404, got %d: %s", rr.Code, rr.Body.String())
	}
}
