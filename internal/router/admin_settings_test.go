package router_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const adminPassword = "admin-password"

func TestAdminSettings_SuperadminOnly(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	userSuffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, userSuffix)
	inviteEmail := "invitee_" + suffix + "@test.invalid"
	invID, _ := testutil.SeedInvitation(t, db, inviteEmail, time.Now().Add(time.Hour))

	admin := superadminJWT(t, adminID, "testadmin_"+suffix)
	user := makeJWT(t, userID, "testuser_"+userSuffix)

	page := serve(h, browserRequest(http.MethodGet, "/admin/settings", admin, nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), inviteEmail) {
		t.Errorf("admin page: got %d, want 200 listing the invitation", page.Code)
	}
	if rr := serve(h, browserRequest(http.MethodGet, "/admin/settings", user, nil)); rr.Code != http.StatusForbidden {
		t.Errorf("admin page as a regular user: got %d, want 403", rr.Code)
	}
	if rr := serve(h, browserRequest(http.MethodGet, "/admin/settings", "", nil)); rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Errorf("admin page signed out: got %d, want a redirect to sign in", rr.Code)
	}

	invPath := "/api/admin/invitations/" + strconv.FormatInt(invID, 10)
	for name, req := range map[string]*http.Request{
		"update setting":    browserRequest(http.MethodPost, "/api/admin/settings", user, url.Values{"key": {"k"}, "value": {"v"}}),
		"create invitation": browserRequest(http.MethodPost, "/api/admin/invitations", user, url.Values{"email": {"x@test.invalid"}}),
		"delete invitation": browserRequest(http.MethodDelete, invPath, user, nil),
	} {
		if rr := serve(h, req); rr.Code != http.StatusForbidden {
			t.Errorf("%s as a regular user: got %d, want 403", name, rr.Code)
		}
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM invitations WHERE id = $1`, invID); n != 1 {
		t.Error("a regular user deleted an invitation")
	}
}

func TestAdminSettings_UpdateNeedsConfirmation(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, adminPassword)
	admin := superadminJWT(t, adminID, "testadmin_"+suffix)
	key := "test_marker_" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM site_settings WHERE key = $1`, key) })
	form := url.Values{"key": {key}, "value": {"on"}}

	for name, f := range map[string]url.Values{
		"no password":    form,
		"wrong password": withPassword(form, "not-the-password"),
	} {
		rr := serve(h, browserRequest(http.MethodPost, "/api/admin/settings", admin, f))
		if rr.Code < 400 {
			t.Errorf("%s: got %d, want a refusal", name, rr.Code)
		}
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM site_settings WHERE key = $1`, key); n != 0 {
		t.Fatal("an unconfirmed request changed the setting")
	}

	rr := serve(h, browserRequest(http.MethodPost, "/api/admin/settings", admin, withPassword(form, adminPassword)))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/admin/settings" {
		t.Errorf("confirmed update: got %d to %q, want 303 to /admin/settings", rr.Code, rr.Header().Get("Location"))
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM site_settings WHERE key = $1 AND value = 'on'`, key); n != 1 {
		t.Error("confirmed update did not store the setting")
	}

	rr = serve(h, htmxRequest(browserRequest(http.MethodPost, "/api/admin/settings", admin, withPassword(url.Values{"key": {key}, "value": {"off"}}, adminPassword))))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), key) {
		t.Errorf("HTMX update: got %d, want the settings fragment listing %s", rr.Code, key)
	}
	if got, _ := svc.SiteSetting.GetAll(t.Context()); len(got) == 0 {
		t.Error("settings are empty after an update")
	}
}

func TestAdminInvitations_CreateAndDelete(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, adminPassword)
	admin := superadminJWT(t, adminID, "testadmin_"+suffix)
	email := "invited_" + suffix + "@test.invalid"
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM invitations WHERE email = $1`, email) })

	if rr := serve(h, browserRequest(http.MethodPost, "/api/admin/invitations", admin, withPassword(url.Values{}, adminPassword))); rr.Code != http.StatusBadRequest {
		t.Errorf("no email: got %d, want 400", rr.Code)
	}
	if rr := serve(h, browserRequest(http.MethodPost, "/api/admin/invitations", admin, url.Values{"email": {email}})); rr.Code < 400 {
		t.Errorf("no password: got %d, want a refusal", rr.Code)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM invitations WHERE email = $1`, email); n != 0 {
		t.Fatal("an unconfirmed request created an invitation")
	}

	rr := serve(h, browserRequest(http.MethodPost, "/api/admin/invitations", admin, withPassword(url.Values{"email": {email}}, adminPassword)))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/admin/settings" {
		t.Fatalf("create: got %d to %q, want 303 to /admin/settings", rr.Code, rr.Header().Get("Location"))
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM invitations WHERE email = $1 AND invited_by_id = $2`, email, adminID).Scan(&id); err != nil {
		t.Fatalf("invitation not stored: %v", err)
	}
	path := "/api/admin/invitations/" + strconv.FormatInt(id, 10)

	if rr := serve(h, browserRequest(http.MethodDelete, "/api/admin/invitations/abc", admin, nil)); rr.Code != http.StatusBadRequest {
		t.Errorf("delete with a bad id: got %d, want 400", rr.Code)
	}
	rr = serve(h, htmxRequest(browserRequest(http.MethodDelete, path, admin, nil)))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), email) {
		t.Errorf("HTMX delete: got %d, want the invitations fragment without %s", rr.Code, email)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM invitations WHERE id = $1`, id); n != 0 {
		t.Error("delete left the invitation behind")
	}

	rr = serve(h, htmxRequest(browserRequest(http.MethodPost, "/api/admin/invitations", admin, withPassword(url.Values{"email": {email}}, adminPassword))))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), email) {
		t.Errorf("HTMX create: got %d, want the invitations fragment listing %s", rr.Code, email)
	}
	if err := db.QueryRow(`SELECT id FROM invitations WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if rr := serve(h, browserRequest(http.MethodDelete, "/api/admin/invitations/"+strconv.FormatInt(id, 10), admin, nil)); rr.Code != http.StatusSeeOther {
		t.Errorf("plain delete: got %d, want 303", rr.Code)
	}
}

func TestAdminVerifyEmail_RejectsBlankFieldsAndRedirectsPlainForms(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, adminPassword)
	targetID := testutil.SeedUser(t, db, suffix)
	admin := superadminJWT(t, adminID, "testadmin_"+suffix)

	if rr := serve(h, browserRequest(http.MethodPost, "/api/admin/users/verify-email", admin, withPassword(url.Values{"username": {" "}, "email": {"a@test.invalid"}}, adminPassword))); rr.Code != http.StatusBadRequest {
		t.Errorf("blank username: got %d, want 400", rr.Code)
	}
	if rr := serve(h, browserRequest(http.MethodPost, "/api/admin/users/verify-email", admin, withPassword(url.Values{"username": {"testuser_" + suffix}}, adminPassword))); rr.Code != http.StatusBadRequest {
		t.Errorf("missing email: got %d, want 400", rr.Code)
	}
	form := withPassword(url.Values{"username": {"testuser_" + suffix}, "email": {"testuser_" + suffix + "@test.invalid"}}, adminPassword)
	rr := serve(h, browserRequest(http.MethodPost, "/api/admin/users/verify-email", admin, form))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/admin/settings" {
		t.Errorf("plain form: got %d to %q, want 303 to /admin/settings", rr.Code, rr.Header().Get("Location"))
	}
	if !isEmailVerified(t, db, targetID) {
		t.Error("the address was not verified")
	}
	takeVerifyAudit(t, db, targetID)
}
