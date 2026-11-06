package router_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const adminModeCookieName = "cz_token_admin"

func TestAdminMode_EndToEnd(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, adminPassword)
	adminName := "testadmin_" + suffix
	ownerSuffix, readerSuffix := suffix+"o", suffix+"r"
	ownerID := testutil.SeedUser(t, db, ownerSuffix)
	readerID := testutil.SeedUser(t, db, readerSuffix)
	ownerName := "testuser_" + ownerSuffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	repoPath := "/" + ownerName + "/testrepo_" + suffix
	testutil.Exec(t, db, `INSERT INTO repositories (owner_id, owner_name, name, description, private) VALUES ($1, $2, $3, '', true)`,
		ownerID, ownerName, "private_"+suffix)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE name = $1`, "private_"+suffix) })
	privPath := "/" + ownerName + "/private_" + suffix
	orgName := "testorg" + strings.ReplaceAll(suffix, "_", "")
	testutil.Exec(t, db, `INSERT INTO organizations (name) VALUES ($1)`, orgName)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, orgName) })
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) SELECT id, $2, 'owner' FROM organizations WHERE name = $1`, orgName, ownerID)
	_ = repoID

	admin := superadminJWT(t, adminID, adminName)
	owner := makeJWT(t, ownerID, ownerName)
	reader := makeJWT(t, readerID, "testuser_"+readerSuffix)

	status := func(req *http.Request) int { return serve(h, req).Code }
	profile := url.Values{"display_name": {"Set By Admin"}}

	// Outside admin mode a superadmin is a normal user.
	for name, path := range map[string]string{"repo settings": repoPath + "/settings", "org settings": "/orgs/" + orgName + "/settings"} {
		if got := status(browserRequest(http.MethodGet, path, admin, nil)); got != http.StatusForbidden {
			t.Errorf("%s as a superadmin outside admin mode: got %d, want 403", name, got)
		}
		if got := status(browserRequest(http.MethodGet, path, reader, nil)); got != http.StatusForbidden {
			t.Errorf("%s as a reader: got %d, want 403", name, got)
		}
		if got := status(browserRequest(http.MethodGet, path, owner, nil)); got != http.StatusOK {
			t.Errorf("%s as the owner: got %d, want 200", name, got)
		}
	}
	if got := status(browserRequest(http.MethodGet, privPath+"/settings", admin, nil)); got != http.StatusNotFound {
		t.Errorf("private repo settings outside admin mode: got %d, want 404", got)
	}

	// Entering needs the confirmation.
	enter := func(form url.Values) *http.Response {
		return serve(h, browserRequest(http.MethodPost, "/admin/mode", admin, form)).Result()
	}
	if resp := enter(url.Values{"password": {"wrong"}}); resp.StatusCode < 400 || len(resp.Cookies()) != 0 {
		t.Errorf("wrong password: got %d with cookies %v, want a refusal and no cookie", resp.StatusCode, resp.Cookies())
	}
	resp := enter(url.Values{"password": {adminPassword}, "next": {repoPath + "/settings"}})
	var modeCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == adminModeCookieName {
			modeCookie = c
		}
	}
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != repoPath+"/settings" || modeCookie == nil {
		t.Fatalf("enter: got %d to %q with cookie %v, want 303 to the settings page and an admin-mode cookie", resp.StatusCode, resp.Header.Get("Location"), modeCookie)
	}
	if !modeCookie.HttpOnly || modeCookie.Value == "" {
		t.Error("the admin-mode cookie must be HttpOnly and hold a token")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM admin_modes WHERE user_id = $1 AND token_hash = $2`, adminID, modeCookie.Value); n != 0 {
		t.Error("the raw cookie token is stored")
	}
	withMode := func(req *http.Request) *http.Request { req.AddCookie(modeCookie); return req }

	// In admin mode, in this browser, the settings open and change.
	for name, path := range map[string]string{"repo settings": repoPath + "/settings", "private repo settings": privPath + "/settings", "org settings": "/orgs/" + orgName + "/settings"} {
		if got := status(withMode(browserRequest(http.MethodGet, path, admin, nil))); got != http.StatusOK {
			t.Errorf("%s in admin mode: got %d, want 200", name, got)
		}
	}
	if got := status(browserRequest(http.MethodPost, "/api/orgs/"+orgName+"/profile", admin, profile)); got < 400 {
		t.Errorf("org change outside admin mode: got %d, want a refusal", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM organizations WHERE name = $1 AND display_name = 'Set By Admin'`, orgName); n != 0 {
		t.Fatal("an org change outside admin mode applied")
	}
	if got := status(withMode(browserRequest(http.MethodPost, "/api/orgs/"+orgName+"/profile", admin, profile))); got >= 400 {
		t.Errorf("org change in admin mode: got %d, want success", got)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM organizations WHERE name = $1 AND display_name = 'Set By Admin'`, orgName); n != 1 {
		t.Error("the admin-mode change did not apply")
	}

	// Another browser, a bearer-header session and git don't get it.
	if got := status(browserRequest(http.MethodGet, repoPath+"/settings", admin, nil)); got != http.StatusForbidden {
		t.Errorf("another browser: got %d, want 403", got)
	}
	bearer := browserRequest(http.MethodGet, repoPath+"/settings", "", nil)
	bearer.Header.Set("Authorization", "Bearer "+admin)
	if got := status(withMode(bearer)); got != http.StatusForbidden {
		t.Errorf("session JWT as a bearer header with the admin cookie: got %d, want 403", got)
	}
	if got := status(withMode(browserRequest(http.MethodGet, privPath+".git/info/refs?service=git-upload-pack", admin, nil))); got == http.StatusOK {
		t.Error("git over HTTP honoured admin mode for a private repo")
	}

	// Entering and the change are audited.
	waitForAudit(t, db, adminID, "admin.mode.enter", 1)
	waitForAudit(t, db, adminID, "admin.mode.action", 1)

	// The viewer's own repos need nothing: reading a repo does not log.
	before := countRows(t, db, `SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action = 'admin.mode.action'`, adminID)
	status(withMode(browserRequest(http.MethodGet, privPath+"/settings", admin, nil)))
	time.Sleep(200 * time.Millisecond)
	if after := countRows(t, db, `SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action = 'admin.mode.action'`, adminID); after != before {
		t.Error("viewing under admin mode must not be logged")
	}

	// Leaving ends it at once.
	if got := status(withMode(browserRequest(http.MethodPost, "/admin/mode/leave", admin, url.Values{}))); got >= 400 {
		t.Errorf("leave: got %d", got)
	}
	if got := status(withMode(browserRequest(http.MethodGet, repoPath+"/settings", admin, nil))); got != http.StatusForbidden {
		t.Errorf("after leaving: got %d, want 403", got)
	}
	waitForAudit(t, db, adminID, "admin.mode.leave", 1)
}

// waitForAudit polls for audit rows, which AuditService.Record writes in the background.
func waitForAudit(t *testing.T, db *sql.DB, actorID int64, action string, want int) {
	t.Helper()
	var n int
	for i := 0; i < 40; i++ {
		n = countRows(t, db, `SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action = $2`, actorID, action)
		if n >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("audit rows for %s: got %d, want %d", action, n, want)
}

func TestAdminMode_LogoutEndsItAndTheActionIsAuditedWithItsRequest(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, adminPassword)
	ownerID := testutil.SeedUser(t, db, suffix+"o")
	orgName := "testorg" + strings.ReplaceAll(suffix, "_", "")
	testutil.Exec(t, db, `INSERT INTO organizations (name) VALUES ($1)`, orgName)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, orgName) })
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) SELECT id, $2, 'owner' FROM organizations WHERE name = $1`, orgName, ownerID)
	settingsPath := "/orgs/" + orgName + "/settings"
	admin := superadminJWT(t, adminID, "testadmin_"+suffix)

	resp := serve(h, browserRequest(http.MethodPost, "/admin/mode", admin, url.Values{"password": {adminPassword}})).Result()
	var mode *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == adminModeCookieName {
			mode = c
		}
	}
	if mode == nil {
		t.Fatalf("enter: got %d without a cookie", resp.StatusCode)
	}
	withMode := func(req *http.Request) *http.Request { req.AddCookie(mode); return req }

	serve(h, withMode(browserRequest(http.MethodPost, "/api/orgs/"+orgName+"/profile", admin, url.Values{"display_name": {"x"}})))
	waitForAudit(t, db, adminID, "admin.mode.action", 1)
	var meta string
	if err := db.QueryRow(`SELECT metadata::text FROM audit_log WHERE actor_id = $1 AND action = 'admin.mode.action'`, adminID).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"method": "POST"`, `"path": "/api/orgs/` + orgName + `/profile"`, `"target": "` + orgName + `"`, `"target_type": "org"`} {
		if !strings.Contains(meta, want) {
			t.Errorf("audit metadata %s lacks %s", meta, want)
		}
	}

	serve(h, withMode(browserRequest(http.MethodPost, "/api/auth/logout", admin, url.Values{})))
	if got := serve(h, withMode(browserRequest(http.MethodGet, settingsPath, admin, nil))).Code; got != http.StatusForbidden {
		t.Errorf("after logout the old cookie still works: got %d, want 403", got)
	}
	waitForAudit(t, db, adminID, "admin.mode.leave", 1)
}
