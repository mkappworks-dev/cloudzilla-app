package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func adminRequest(t *testing.T, h http.Handler, method, path, token string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func suspendedAt(t *testing.T, db *sql.DB, id int64) bool {
	t.Helper()
	var suspended bool
	if err := db.QueryRow(`SELECT suspended_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&suspended); err != nil {
		t.Fatal(err)
	}
	return suspended
}

func TestAdminUsers_NonSuperadminForbidden(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, sfx)
	target := testutil.SeedUser(t, db, "target_"+sfx)
	token := makeIssueJWT(t, userID, "testuser_"+sfx)

	for _, path := range []string{"/admin/users", "/admin/users/testuser_target_" + sfx} {
		if rr := adminRequest(t, h, http.MethodGet, path, token, nil); rr.Code != http.StatusForbidden {
			t.Errorf("GET %s: want 403, got %d", path, rr.Code)
		}
	}
	rr := adminRequest(t, h, http.MethodPost, "/api/admin/users/testuser_target_"+sfx+"/suspend", token, url.Values{})
	if rr.Code != http.StatusForbidden || suspendedAt(t, db, target) {
		t.Errorf("suspend by non-superadmin: %d, suspended %v", rr.Code, suspendedAt(t, db, target))
	}
}

func TestAdminUsers_ListAndUserPage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SeedUser(t, db, "listed_"+sfx)
	// The JWT doesn't claim superadmin; the role is read from the account.
	token := makeIssueJWT(t, adminID, "testadmin_"+sfx)

	rr := adminRequest(t, h, http.MethodGet, "/admin/users?q=testuser_listed_"+sfx, token, nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "testuser_listed_"+sfx) {
		t.Errorf("list: %d, contains user %v", rr.Code, strings.Contains(rr.Body.String(), "testuser_listed_"+sfx))
	}
	rr = adminRequest(t, h, http.MethodGet, "/admin/users/testuser_listed_"+sfx, token, nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Revoke tokens and keys") {
		t.Errorf("user page: %d", rr.Code)
	}
	var ghost string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = ghost_user_id()`).Scan(&ghost); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{ghost, "no_such_user_" + sfx} {
		if rr := adminRequest(t, h, http.MethodGet, "/admin/users/"+name, token, nil); rr.Code != http.StatusNotFound {
			t.Errorf("GET /admin/users/%s: want 404, got %d", name, rr.Code)
		}
	}
}

func TestAdminUsers_SuspendNeedsPasswordAndIsAudited(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	target := testutil.SeedUser(t, db, "target_"+sfx)
	token := makeIssueJWT(t, adminID, "testadmin_"+sfx)
	path := "/api/admin/users/testuser_target_" + sfx + "/suspend"
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM audit_log WHERE actor_id = $1`, adminID) })

	if rr := adminRequest(t, h, http.MethodPost, path, token, url.Values{"password": {"wrong"}}); rr.Code != http.StatusForbidden || suspendedAt(t, db, target) {
		t.Fatalf("wrong password: %d, suspended %v", rr.Code, suspendedAt(t, db, target))
	}
	rr := adminRequest(t, h, http.MethodPost, path, token, url.Values{"password": {"admin-password"}, "reason": {"left the company"}})
	if rr.Code != http.StatusSeeOther || !suspendedAt(t, db, target) {
		t.Fatalf("suspend: %d %s, suspended %v", rr.Code, rr.Body.String(), suspendedAt(t, db, target))
	}

	// Audit entries are written in the background.
	var reason string
	for range 50 {
		err := db.QueryRow(`SELECT metadata->>'reason' FROM audit_log WHERE actor_id = $1 AND action = $2 AND target_id = $3`,
			adminID, model.AuditActionAdminUserSuspend, target).Scan(&reason)
		if err == nil {
			break
		}
		testutil.Exec(t, db, `SELECT pg_sleep(0.02)`)
	}
	if reason != "left the company" {
		t.Errorf("audit reason = %q", reason)
	}

	rr = adminRequest(t, h, http.MethodPost, "/api/admin/users/testadmin_"+sfx+"/suspend", token, url.Values{"password": {"admin-password"}})
	if rr.Code != http.StatusForbidden || suspendedAt(t, db, adminID) {
		t.Errorf("self-suspend: %d, suspended %v", rr.Code, suspendedAt(t, db, adminID))
	}
}

func TestAdminUsers_DemoteAndDelete(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	other := testutil.SeedSuperadmin(t, db, "other_"+sfx)
	token := makeIssueJWT(t, adminID, "testadmin_"+sfx)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM audit_log WHERE actor_id = $1`, adminID) })
	otherName := "testadmin_other_" + sfx

	rr := adminRequest(t, h, http.MethodPost, "/api/admin/users/"+otherName+"/demote", token, url.Values{"password": {"admin-password"}})
	var superadmin bool
	if err := db.QueryRow(`SELECT is_superadmin FROM users WHERE id = $1`, other).Scan(&superadmin); err != nil || rr.Code != http.StatusSeeOther || superadmin {
		t.Fatalf("demote: %d, superadmin %v (%v)", rr.Code, superadmin, err)
	}

	rr = adminRequest(t, h, http.MethodPost, "/api/admin/users/"+otherName+"/delete", token, url.Values{"password": {"admin-password"}, "confirm_username": {"nope"}})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("mistyped delete: want 400, got %d", rr.Code)
	}
	rr = adminRequest(t, h, http.MethodPost, "/api/admin/users/"+otherName+"/delete", token, url.Values{"password": {"admin-password"}, "confirm_username": {otherName}})
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = $1`, other).Scan(&n); err != nil || rr.Code != http.StatusSeeOther || n != 0 {
		t.Errorf("delete: %d %s, rows left %d (%v)", rr.Code, rr.Body.String(), n, err)
	}
}
