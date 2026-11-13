package handler_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newSigningRouter(t *testing.T, db *sql.DB, secretKey string) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Server:   config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:     config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:      config.GitConfig{ReposRoot: t.TempDir()},
		Security: config.SecurityConfig{SecretKey: secretKey},
	}
	h, err := router.New(service.New(store.New(db), cfg), cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h
}

func activeFingerprint(t *testing.T, db *sql.DB) string {
	t.Helper()
	var fp string
	if err := db.QueryRow(`SELECT fingerprint FROM instance_signing_keys WHERE retired_at IS NULL`).Scan(&fp); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return fp
}

func TestAdminSigningKey_RotateNeedsSuperadminAndStepUpAndIsAudited(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	h := newSigningRouter(t, db, "0123456789abcdef0123456789abcdef")
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	admin := makeIssueJWT(t, adminID, "testadmin_"+sfx)
	user := seedSignedInUser(t, db)
	const route = "/api/admin/signing-key/rotate"

	if rr := adminRequest(t, h, http.MethodPost, route, user.token, url.Values{"password": {"x"}}); rr.Code != http.StatusForbidden {
		t.Errorf("non-superadmin: want 403, got %d", rr.Code)
	}
	if rr := adminRequest(t, h, http.MethodPost, route, admin, url.Values{"password": {"wrong"}}); rr.Code != http.StatusForbidden {
		t.Errorf("wrong password: want 403, got %d", rr.Code)
	}
	if fp := activeFingerprint(t, db); fp != "" {
		t.Fatalf("a refused rotation made key %s", fp)
	}

	if rr := adminRequest(t, h, http.MethodPost, route, admin, url.Values{"password": {"admin-password"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("first rotate: want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	first := activeFingerprint(t, db)
	if first == "" {
		t.Fatal("no active key after the first rotation")
	}
	if rr := adminRequest(t, h, http.MethodPost, route, admin, url.Values{"password": {"admin-password"}}, "HX-Request", "true"); rr.Code != http.StatusNoContent ||
		rr.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("second rotate over HTMX: %d\n%s", rr.Code, rr.Body.String())
	}
	second := activeFingerprint(t, db)
	if second == first {
		t.Fatal("the second rotation kept the same key")
	}

	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'admin.signing_key.rotate' AND actor_id = $1 AND target_name IN ($2, $3)`,
		adminID, first, second).Scan(&audits); err != nil || audits != 2 {
		t.Errorf("audit entries: %d, %v; want 2", audits, err)
	}

	page := getPath(t, h, "/admin/settings", admin).Body.String()
	assertContains(t, page, "Commit signing")
	assertContains(t, page, second)
	if strings.Contains(page, first) {
		t.Error("the retired key's fingerprint is shown as the active one")
	}
}

func TestAdminSigningKey_NoSecretKeyExplainsWhyAndRefusesRotate(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	h := newSigningRouter(t, db, "")
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, "admin-password")
	admin := makeIssueJWT(t, adminID, "testadmin_"+sfx)

	page := getPath(t, h, "/admin/settings", admin).Body.String()
	assertContains(t, page, "security.secret_key")
	if strings.Contains(page, "/api/admin/signing-key/rotate") {
		t.Error("the rotate form is offered without a secret key")
	}
	rr := adminRequest(t, h, http.MethodPost, "/api/admin/signing-key/rotate", admin, url.Values{"password": {"admin-password"}})
	if rr.Code != http.StatusConflict {
		t.Errorf("rotate without a secret key: want 409, got %d", rr.Code)
	}
}
