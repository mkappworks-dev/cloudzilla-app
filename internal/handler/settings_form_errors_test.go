package handler_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// The forms' data-toast treats a 200 without HX-Retarget as a save, so every
// refusal must carry it.
func TestSettingsForms_HTMX_Refused_RendersFormError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	router := pageRouter(newPageHandler(t, db))
	user := seedSignedInUser(t, db)
	testutil.SetPassword(t, db, user.id, "user-password")
	org := "org_" + testutil.UniqueSuffix(t)
	seedOrgOwnedBy(t, db, org, user.id)
	adminID := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	testutil.SetPassword(t, db, adminID, "admin-password")
	admin := makeSuperadminJWT(t, adminID, "testadmin")

	cases := []struct {
		name, token, path, slot, msg string
		form                         url.Values
	}{
		{"profile email", user.token, "/settings/profile", "#profile-form-error", "Enter a valid email address.",
			url.Values{"email": {"not-an-email"}, "password": {"user-password"}}},
		{"totp enable", user.token, "/api/user/totp/enable", "#totp-enable-form-error", "Secret and verification code are required.",
			url.Values{"secret": {testutil.TestTOTPSecret}}},
		{"totp disable", user.token, "/api/user/totp/disable", "#totp-disable-form-error", "Verification code is required.",
			url.Values{}},
		{"token name", user.token, "/api/user/tokens", "#generate-token-form-error", "name is required",
			url.Values{"scopes": {"repo:read"}}},
		{"token password", user.token, "/api/user/tokens", "#generate-token-form-error", "Your password or two-factor code was incorrect.",
			url.Values{"name": {"ci"}, "scopes": {"repo:read"}, "password": {"wrong"}}},
		{"org profile", user.token, "/api/orgs/" + org + "/profile", "#org-profile-form-error", "website must be an http or https URL",
			url.Values{"website": {"ftp://example.com"}}},
		{"org repo defaults", user.token, "/api/orgs/" + org + "/repo-defaults", "#org-repo-defaults-form-error", "default visibility must be public or private",
			url.Values{"default_repo_visibility": {"secret"}, "default_branch_name": {"main"}}},
		{"sso password", admin, "/admin/sso", "#sso-ldap-form-error", "Your password or two-factor code was incorrect.",
			url.Values{"provider": {"ldap"}, "ldap_host": {"ldap.test.invalid"}, "password": {"wrong"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := postHXForm(t, router, tc.token, tc.path, tc.form)

			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("HX-Retarget"); got != tc.slot {
				t.Errorf("HX-Retarget = %q, want %q", got, tc.slot)
			}
			if got := rr.Header().Get("HX-Redirect"); got != "" {
				t.Errorf("HX-Redirect = %q on an error", got)
			}
			assertContains(t, rr.Body.String(), tc.msg)
		})
	}
}

// htmx:response:error shows a JSON body's error field; plain text would only
// show the status code.
func TestSaveSSOConfig_HTMX_StoreError_JSONError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	adminID := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	testutil.SetPassword(t, db, adminID, "admin-password")

	rr := postHXForm(t, pageRouter(newPageHandler(t, db)), makeSuperadminJWT(t, adminID, "testadmin"), "/admin/sso",
		url.Values{"provider": {"ldap"}, "ldap_host": {"ldap.test.invalid" + nul}, "password": {"admin-password"}})

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct{ Error string }
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || !strings.Contains(body.Error, "Could not save the SSO configuration") {
		t.Errorf("want a JSON error body, got %q", rr.Body.String())
	}
}

// The data-toast listener stashes the toast for the next page only when the
// response carries HX-Redirect.
func TestSettingsForms_HTMX_Saved_Redirects(t *testing.T) {
	db := testutil.OpenTestDB(t)
	router := pageRouter(newPageHandler(t, db))
	user := seedSignedInUser(t, db)
	testutil.SetPassword(t, db, user.id, "user-password")
	org := "org_" + testutil.UniqueSuffix(t)
	seedOrgOwnedBy(t, db, org, user.id)

	cases := []struct {
		name, path, redirect string
		form                 url.Values
	}{
		{"profile", "/settings/profile", "/settings?profile_saved=1#profile",
			url.Values{"name": {"Renamed"}, "email": {user.name + "@test.invalid"}}},
		{"org profile", "/api/orgs/" + org + "/profile", "/orgs/" + org + "/settings",
			url.Values{"display_name": {"Acme"}}},
		{"org repo defaults", "/api/orgs/" + org + "/repo-defaults", "/orgs/" + org + "/settings#repo-defaults",
			url.Values{"default_repo_visibility": {"public"}, "default_branch_name": {"trunk"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := postHXForm(t, router, user.token, tc.path, tc.form)

			if rr.Code != http.StatusNoContent {
				t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("HX-Redirect"); got != tc.redirect {
				t.Errorf("HX-Redirect = %q, want %q", got, tc.redirect)
			}
		})
	}
}

// The new token is shown once by the page the HX-Redirect loads, so the flash
// cookie has to ride on the redirect response.
func TestCreateToken_HTMX_Saved_RedirectsWithFlash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	user := seedSignedInUser(t, db)
	testutil.SetPassword(t, db, user.id, "user-password")

	rr := postHXForm(t, pageRouter(newPageHandler(t, db)), user.token, "/api/user/tokens",
		url.Values{"name": {"ci"}, "scopes": {"repo:read"}, "password": {"user-password"}})

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/settings#tokens" {
		t.Errorf("HX-Redirect = %q, want /settings#tokens", got)
	}
	var flash bool
	for _, c := range rr.Result().Cookies() {
		flash = flash || (c.Name == "cz_new_token" && c.Value != "")
	}
	if !flash {
		t.Error("no new-token flash cookie on the redirect")
	}
}

func TestSettingsPages_NoPlainPostToastForms(t *testing.T) {
	db := testutil.OpenTestDB(t)
	user := seedSignedInUser(t, db)
	org := "org_" + testutil.UniqueSuffix(t)
	seedOrgOwnedBy(t, db, org, user.id)
	adminID := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))

	for path, tc := range map[string]struct {
		token string
		slots []string
	}{
		"/settings":                  {user.token, []string{`id="profile-form-error"`, `id="generate-token-form-error"`}},
		"/orgs/" + org + "/settings": {user.token, []string{`id="org-profile-form-error"`, `id="org-repo-defaults-form-error"`}},
		"/admin/sso":                 {makeSuperadminJWT(t, adminID, "testadmin"), []string{`id="sso-ldap-form-error"`, `id="sso-saml-form-error"`}},
	} {
		t.Run(path, func(t *testing.T) {
			rr := requestPage(t, db, http.MethodGet, path, tc.token)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rr.Code)
			}
			if m := plainPostToastForm(rr.Body.String()); m != "" {
				t.Errorf("page has a plain POST form with data-toast: %s", m)
			}
			for _, slot := range tc.slots {
				assertContains(t, rr.Body.String(), slot)
			}
		})
	}
}
