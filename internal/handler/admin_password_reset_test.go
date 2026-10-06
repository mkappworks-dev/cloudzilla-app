package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var resetLinkToken = regexp.MustCompile(`/auth/password/reset/([A-Za-z0-9_-]{43})`)

func resetPage(h http.Handler, token string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/auth/password/reset/"+token, nil))
	return rr
}

func TestAdminPasswordResetLink(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	userID, _ := testutil.SeedUserWithPassword(t, db, sfx, "user-password")
	token := makeIssueJWT(t, adminID, "testadmin_"+sfx)
	path := "/api/admin/users/testpw_" + sfx + "/password-reset-link"
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM audit_log WHERE actor_id = $1`, adminID) })
	links := func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = $1`, userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if rr := adminRequest(t, h, http.MethodPost, path, token, url.Values{"password": {"wrong"}}); rr.Code != http.StatusForbidden || links() != 0 {
		t.Fatalf("wrong password: %d, %d links", rr.Code, links())
	}

	rr := adminRequest(t, h, http.MethodPost, path, token, url.Values{"password": {"admin-password"}}, "HX-Request", "true")
	if rr.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", rr.Code, rr.Body.String())
	}
	m := resetLinkToken.FindStringSubmatch(rr.Body.String())
	if m == nil || !strings.Contains(rr.Body.String(), "won't be shown again") {
		t.Fatalf("issue body has no shown-once link: %s", rr.Body.String())
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	resetToken := m[1]

	// Written before the response, so there's nothing to wait for.
	var metadata string
	if err := db.QueryRow(`SELECT metadata FROM audit_log WHERE actor_id = $1 AND action = $2 AND target_id = $3`,
		adminID, model.AuditActionPasswordResetLink, userID).Scan(&metadata); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil || meta["issued_by"] != model.PasswordResetByAdmin || strings.Contains(metadata, resetToken) {
		t.Errorf("audit metadata = %s, want issued_by admin and no token", metadata)
	}

	if rr := resetPage(h, resetToken); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "testpw_"+sfx) {
		t.Errorf("reset page: %d", rr.Code)
	}
	page := adminRequest(t, h, http.MethodGet, "/admin/users/testpw_"+sfx, token, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Password reset link") || strings.Contains(page.Body.String(), resetToken) {
		t.Errorf("user page after issue: %d, has row %v, shows token %v", page.Code,
			strings.Contains(page.Body.String(), "Password reset link"), strings.Contains(page.Body.String(), resetToken))
	}

	// Without htmx it's a JSON API.
	rr = adminRequest(t, h, http.MethodPost, path, token, url.Values{"password": {"admin-password"}})
	var body struct {
		Link      string `json:"link"`
		ExpiresAt string `json:"expires_at"`
	}
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &body) != nil || !resetLinkToken.MatchString(body.Link) || body.ExpiresAt == "" {
		t.Fatalf("JSON issue: %d %s", rr.Code, rr.Body.String())
	}
	if rr := resetPage(h, resetToken); rr.Code == http.StatusOK {
		t.Error("a second link left the first one usable")
	}
}

func TestAdminPasswordResetLink_Refusals(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	token := makeIssueJWT(t, adminID, "testadmin_"+sfx)
	suspended, _ := testutil.SeedUserWithPassword(t, db, "s"+sfx, "user-password")
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, suspended)
	passwordless := testutil.SeedPasswordlessUser(t, db, sfx, "g-"+sfx)
	plain := testutil.SeedUser(t, db, sfx)
	plainToken := makeIssueJWT(t, plain, "testuser_"+sfx)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM audit_log WHERE actor_id = $1`, adminID) })
	confirm := url.Values{"password": {"admin-password"}}

	for _, tc := range []struct {
		name, username, token string
		want                  int
	}{
		{"non-superadmin", "testpw_" + sfx, plainToken, http.StatusForbidden},
		{"own account", "testadmin_" + sfx, token, http.StatusForbidden},
		{"suspended", "testpw_s" + sfx, token, http.StatusConflict},
		{"passwordless", "testnopw_" + sfx, token, http.StatusConflict},
		{"unknown", "nobody_" + sfx, token, http.StatusNotFound},
	} {
		rr := adminRequest(t, h, http.MethodPost, "/api/admin/users/"+tc.username+"/password-reset-link", tc.token, confirm)
		if rr.Code != tc.want || resetLinkToken.MatchString(rr.Body.String()) {
			t.Errorf("%s: %d %s, want %d and no link", tc.name, rr.Code, rr.Body.String(), tc.want)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id IN ($1, $2, $3)`, suspended, passwordless, adminID).Scan(&n); err != nil || n != 0 {
		t.Errorf("password_reset_tokens rows = %d, %v; want none", n, err)
	}

	for _, name := range []string{"testpw_s" + sfx, "testnopw_" + sfx} {
		page := adminRequest(t, h, http.MethodGet, "/admin/users/"+name, token, nil)
		if page.Code != http.StatusOK || strings.Contains(page.Body.String(), "Password reset link") {
			t.Errorf("%s page: %d, offers a reset link %v", name, page.Code, strings.Contains(page.Body.String(), "Password reset link"))
		}
	}
}
