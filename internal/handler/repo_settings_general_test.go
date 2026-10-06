package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
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

func postHXForm(t *testing.T, router http.Handler, token, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// The form's data-toast treats a 200 without HX-Retarget as a save, so the
// error must carry it.
func TestUpdateRepoGeneral_HTMX_InvalidDefaultBranch_RendersFormError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	rr := postHXForm(t, pageRouter(newPageHandler(t, db)), repo.owner.token, repo.path+"/settings/general",
		url.Values{"description": {"after"}, "default_branch": {"bad name"}})

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Retarget"); got != "#repo-general-form-error" {
		t.Errorf("HX-Retarget = %q, want #repo-general-form-error", got)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "" {
		t.Errorf("HX-Redirect = %q on an error", got)
	}
	assertContains(t, rr.Body.String(), `&#34;bad name&#34; is not a valid branch name`)
}

// htmx:response:error shows a JSON body's error field; plain text would only
// show the status code.
func TestRepoSettingsForms_HTMX_Forbidden_JSONError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)
	reader := seedSignedInUser(t, db)
	router := pageRouter(newPageHandler(t, db))

	for path, form := range map[string]url.Values{
		"/settings/general":    {"description": {"after"}, "default_branch": {"main"}},
		"/settings/features":   {"allow_issues": {"on"}},
		"/settings/visibility": {"private": {"true"}},
	} {
		t.Run(path, func(t *testing.T) {
			rr := postHXForm(t, router, reader.token, repo.path+path, form)

			if rr.Code != http.StatusForbidden {
				t.Fatalf("want 403, got %d: %s", rr.Code, rr.Body.String())
			}
			var body struct{ Error string }
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body.Error == "" {
				t.Errorf("want a JSON error body, got %q", rr.Body.String())
			}
		})
	}
}

// The data-toast listener stashes the toast for the next page only when the
// response carries HX-Redirect.
func TestUpdateRepoFeatures_HTMX_Saved_Redirects(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	rr := postHXForm(t, pageRouter(newPageHandler(t, db)), repo.owner.token, repo.path+"/settings/features",
		url.Values{"allow_issues": {"on"}})

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if got, want := rr.Header().Get("HX-Redirect"), repo.path+"/settings"; got != want {
		t.Errorf("HX-Redirect = %q, want %q", got, want)
	}
}

var plainPostToastForm = regexp.MustCompile(`<form[^>]*method="POST"[^>]*data-toast`)

// A plain POST form stashes its success toast on submit, before the server
// answers, so a failed save would show it on the next page.
func TestPageRepoSettings_NoPlainPostToastForms(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	rr := requestPage(t, db, http.MethodGet, repo.path+"/settings", repo.owner.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("settings page: want 200, got %d", rr.Code)
	}
	if m := plainPostToastForm.FindString(rr.Body.String()); m != "" {
		t.Errorf("settings page has a plain POST form with data-toast: %s", m)
	}
	assertContains(t, rr.Body.String(), `id="repo-general-form-error"`)
}
