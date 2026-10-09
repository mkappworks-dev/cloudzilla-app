package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestAdminOrgOwner_AddNeedsSuperadminAndPassword(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouter(t, db)
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	adminToken := makeIssueJWT(t, adminID, "testadmin_"+sfx)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM audit_log WHERE actor_id = $1`, adminID) })

	oldOwner := testutil.SeedUser(t, db, "old_"+sfx)
	newOwner := testutil.SeedUser(t, db, "new_"+sfx)
	suspended := testutil.SeedUser(t, db, "sus_"+sfx)
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, suspended)
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, "testorg_"+sfx).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, orgID, oldOwner)

	owns := func(id int64) bool {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND user_id = $2 AND role = 'owner'`, orgID, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	path := "/api/admin/orgs/testorg_" + sfx + "/owners"
	form := func(username, password string) url.Values {
		return url.Values{"username": {username}, "password": {password}, "from": {"testuser_old_" + sfx}}
	}

	userToken := makeIssueJWT(t, newOwner, "testuser_new_"+sfx)
	if rr := adminRequest(t, h, http.MethodPost, path, userToken, form("testuser_new_"+sfx, "x")); rr.Code != http.StatusForbidden || owns(newOwner) {
		t.Errorf("non-superadmin: %d, owns %v", rr.Code, owns(newOwner))
	}
	if rr := adminRequest(t, h, http.MethodPost, path, adminToken, form("testuser_new_"+sfx, "wrong")); rr.Code != http.StatusForbidden || owns(newOwner) {
		t.Errorf("wrong password: %d, owns %v", rr.Code, owns(newOwner))
	}
	if rr := adminRequest(t, h, http.MethodPost, path, adminToken, form("testuser_sus_"+sfx, "admin-password")); rr.Code != http.StatusConflict || owns(suspended) {
		t.Errorf("suspended target: %d, owns %v", rr.Code, owns(suspended))
	}
	if rr := adminRequest(t, h, http.MethodPost, path, adminToken, form("nobody_"+sfx, "admin-password")); rr.Code != http.StatusNotFound {
		t.Errorf("unknown user: %d", rr.Code)
	}
	if rr := adminRequest(t, h, http.MethodPost, "/api/admin/orgs/noorg_"+sfx+"/owners", adminToken, form("testuser_new_"+sfx, "admin-password")); rr.Code != http.StatusNotFound {
		t.Errorf("unknown org: %d", rr.Code)
	}

	rr := adminRequest(t, h, http.MethodPost, path, adminToken, form("testuser_new_"+sfx, "admin-password"))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/admin/users/testuser_old_"+sfx || !owns(newOwner) {
		t.Fatalf("add owner: %d %s -> %q, owns %v", rr.Code, rr.Body.String(), rr.Header().Get("Location"), owns(newOwner))
	}
	var members int
	if err := db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, adminID).Scan(&members); err != nil || members != 0 {
		t.Errorf("the admin must not join the org: %d (%v)", members, err)
	}

	var username string
	for range 50 {
		err := db.QueryRow(`SELECT metadata->>'username' FROM audit_log WHERE actor_id = $1 AND action = $2 AND target_type = 'org' AND target_id = $3`,
			adminID, model.AuditActionAdminOrgOwnerAdd, orgID).Scan(&username)
		if err == nil {
			break
		}
		testutil.Exec(t, db, `SELECT pg_sleep(0.02)`)
	}
	if username != "testuser_new_"+sfx {
		t.Errorf("audit username = %q", username)
	}

	// The user page offers the action for each org the user solely owns.
	testutil.Exec(t, db, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, newOwner)
	page := adminRequest(t, h, http.MethodGet, "/admin/users/testuser_old_"+sfx, adminToken, nil)
	if !strings.Contains(page.Body.String(), "/api/admin/orgs/testorg_"+sfx+"/owners") {
		t.Errorf("user page has no add-owner form for the sole-owned org")
	}
}
