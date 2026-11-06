package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsFormEncoded(t *testing.T) {
	for _, tc := range []struct {
		contentType string
		want        bool
	}{
		{"application/x-www-form-urlencoded", true},
		{"application/x-www-form-urlencoded;charset=UTF-8", true},
		{"Application/X-WWW-Form-Urlencoded; charset=utf-8", true},
		{"multipart/form-data; boundary=x", false},
		{"application/json", false},
		{"", false},
	} {
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("Content-Type", tc.contentType)
		if got := isFormEncoded(req); got != tc.want {
			t.Errorf("isFormEncoded(%q) = %v, want %v", tc.contentType, got, tc.want)
		}
	}
}

// A browser treats a redirect to its own URL plus a hash as a scroll, not a
// load, so a save that lands back on its own page must reload it instead.
func TestRedirectAfterSave(t *testing.T) {
	for _, tc := range []struct {
		name, current, to string
		wantRefresh       bool
	}{
		{"hash on the same page", "http://cz.test/settings", "/settings#tokens", true},
		{"same page and hash", "http://cz.test/settings#security", "/settings#security", true},
		{"other hash on the same page", "http://cz.test/settings#profile", "/settings#security", true},
		{"org settings section", "http://cz.test/orgs/paper-planes/settings", "/orgs/paper-planes/settings#repo-defaults", true},
		{"same query and hash", "http://cz.test/settings?profile_saved=1#profile", "/settings?profile_saved=1#profile", true},
		{"query differs", "http://cz.test/settings", "/settings?profile_saved=1#profile", false},
		{"no hash", "http://cz.test/settings#tokens", "/settings", false},
		{"other page", "http://cz.test/acme/app/blob/main/a.txt", "/acme/app/tree/main", false},
		{"no current URL", "", "/settings#tokens", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/user/tokens", nil)
			req.Header.Set("HX-Request", "true")
			if tc.current != "" {
				req.Header.Set("HX-Current-URL", tc.current)
			}
			rr := httptest.NewRecorder()
			redirectAfterSave(rr, req, tc.to)

			if rr.Code != http.StatusNoContent {
				t.Errorf("status = %d, want 204", rr.Code)
			}
			refresh, redirect := rr.Header().Get("HX-Refresh"), rr.Header().Get("HX-Redirect")
			if tc.wantRefresh && (refresh != "true" || redirect != "") {
				t.Errorf("HX-Refresh = %q, HX-Redirect = %q; want a refresh", refresh, redirect)
			}
			if !tc.wantRefresh && (refresh != "" || redirect != tc.to) {
				t.Errorf("HX-Refresh = %q, HX-Redirect = %q; want a redirect to %s", refresh, redirect, tc.to)
			}
		})
	}
}

func TestRedirectAfterSave_PlainForm(t *testing.T) {
	req := httptest.NewRequest("POST", "/settings/totp/disable", nil)
	rr := httptest.NewRecorder()
	redirectAfterSave(rr, req, "/settings#security")
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings#security" {
		t.Errorf("got %d Location %q, want 303 to /settings#security", rr.Code, rr.Header().Get("Location"))
	}
}
