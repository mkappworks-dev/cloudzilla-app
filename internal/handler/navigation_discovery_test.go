package handler_test

// Integration tests: the terms page, the no-SMTP recovery path, Explore filters and repo-scoped navigation.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func getPath(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestTerms_DefaultThenAdminEdit(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	admin := makeIssueJWT(t, adminID, "testadmin_"+sfx)
	user := seedSignedInUser(t, db)

	rr := getPath(t, h, "/terms", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Terms of Service") {
		t.Fatalf("anonymous GET /terms: %d, want the default text", rr.Code)
	}

	save := func(token, password, terms string) *httptest.ResponseRecorder {
		return adminRequest(t, h, http.MethodPost, "/api/admin/terms", token,
			url.Values{"terms": {terms}, "password": {password}})
	}
	if rr := save(user.token, "x", "# Mine"); rr.Code != http.StatusForbidden {
		t.Errorf("non-admin save: want 403, got %d", rr.Code)
	}
	if rr := save(admin, "wrong", "# Mine"); rr.Code != http.StatusForbidden {
		t.Errorf("wrong password: want 403, got %d", rr.Code)
	}

	if rr := save(admin, "admin-password", "# House rules\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))"); rr.Code != http.StatusSeeOther {
		t.Fatalf("save: want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	body := getPath(t, h, "/terms", "").Body.String()
	if !strings.Contains(body, "House rules") {
		t.Errorf("saved terms not shown:\n%s", body)
	}
	if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, `href="javascript:`) {
		t.Errorf("terms rendered unsafe HTML:\n%s", body)
	}

	settings := getPath(t, h, "/admin/settings", admin).Body.String()
	if strings.Contains(settings, `id="setting-terms_content"`) {
		t.Error("terms text must not appear as a feature toggle")
	}
	assertContains(t, settings, `name="terms"`)

	if rr := save(admin, "admin-password", "  "); rr.Code != http.StatusSeeOther {
		t.Fatalf("clear: want 303, got %d", rr.Code)
	}
	if body := getPath(t, h, "/terms", "").Body.String(); strings.Contains(body, "House rules") || !strings.Contains(body, "Terms of Service") {
		t.Errorf("clearing should restore the default text:\n%s", body)
	}

	if rr := save(admin, "admin-password", strings.Repeat("a", 50_001)); rr.Code != http.StatusBadRequest {
		t.Errorf("oversized terms: want 400, got %d", rr.Code)
	}
}

func TestTerms_LinkedFromFooterAndNewOrganization(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	h := newAPIRouter(t, db)
	user := seedSignedInUser(t, db)

	assertContains(t, getPath(t, h, "/login", "").Body.String(), `href="/terms"`)
	assertContains(t, getPath(t, h, "/organizations/new", user.token).Body.String(), `href="/terms"`)
}

func TestForgotPassword_WithoutSMTPNamesAdministrators(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	testutil.SeedSuperadmin(t, db, sfx)

	forgot := getPath(t, h, "/auth/password/forgot", "").Body.String()
	assertContains(t, forgot, "Ask an administrator")
	assertContains(t, forgot, `href="/testadmin_`+sfx+`"`)
	if strings.Contains(forgot, "Send reset link") {
		t.Error("no-SMTP page must not offer the email form")
	}

	assertContains(t, getPath(t, h, "/login", "").Body.String(), `href="/auth/password/forgot"`)
}

func TestExplore_FiltersByTextAndLanguage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	goRepo := seedOwnedRepo(t, db, false)
	rustRepo := seedOwnedRepo(t, db, false)
	testutil.Exec(t, db, `UPDATE repositories SET primary_language = 'Go' WHERE id = $1`, goRepo.id)
	testutil.Exec(t, db, `UPDATE repositories SET primary_language = 'Rust' WHERE id = $1`, rustRepo.id)

	byText := getPath(t, h, "/explore?tab=newest&q="+url.QueryEscape(goRepo.name), "").Body.String()
	assertContains(t, byText, goRepo.path)
	if strings.Contains(byText, rustRepo.path) {
		t.Error("text filter listed an unrelated repository")
	}

	byLang := getPath(t, h, "/explore?tab=newest&language=Rust&q=testrepo_", "").Body.String()
	assertContains(t, byLang, rustRepo.path)
	if strings.Contains(byLang, goRepo.path) {
		t.Error("language filter listed a repository in another language")
	}

	assertContains(t, getPath(t, h, "/explore?tab=newest", "").Body.String(), `<option value="Rust"`)
}

func TestStargazers_ShowsRepositorySubnav(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)

	body := getPath(t, h, repo.path+"/stargazers", repo.owner.token).Body.String()
	assertContains(t, body, `aria-label="Repository sections"`)
}
