package router_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const ldapBindDNTmpl = "uid=%s,ou=people,dc=test"

// ldapBindSuccess is an LDAPMessage carrying a BindResponse for message 1
// with resultCode success and empty matchedDN and diagnosticMessage.
var ldapBindSuccess = []byte{0x30, 0x0c, 0x02, 0x01, 0x01, 0x61, 0x07, 0x0a, 0x01, 0x00, 0x04, 0x00, 0x04, 0x00}

// fakeLDAP starts a server that accepts every simple bind, whatever the DN and
// password, and returns its address.
func fakeLDAP(t *testing.T) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				if _, err := conn.Read(make([]byte, 512)); err == nil {
					_, _ = conn.Write(ldapBindSuccess)
				}
			}()
		}
	}()
	host, port, _ = net.SplitHostPort(ln.Addr().String())
	return host, port
}

// enableLDAP points the instance's LDAP config at a fake server for the rest
// of the test, then restores the config that was there before.
func enableLDAP(t *testing.T, svc *service.Services, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	prior, err := svc.SSO.GetConfig(ctx, "ldap")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	t.Cleanup(func() {
		if prior == nil {
			testutil.Exec(t, db, `DELETE FROM sso_configs WHERE provider = 'ldap'`)
		} else if err := svc.SSO.SetConfig(ctx, "ldap", prior.Config, prior.Enabled); err != nil {
			t.Errorf("restore ldap config: %v", err)
		}
	})
	host, port := fakeLDAP(t)
	cfg := map[string]string{model.LDAPKeyHost: host, model.LDAPKeyPort: port, model.LDAPKeyBindDNTmpl: ldapBindDNTmpl}
	if err := svc.SSO.SetConfig(ctx, "ldap", cfg, true); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
}

// seedLDAPUser seeds a user linked to an LDAP identity and returns the user's
// ID and LDAP uid.
func seedLDAPUser(t *testing.T, db *sql.DB, suffix string) (id int64, uid string) {
	t.Helper()
	id = testutil.SeedUser(t, db, suffix)
	uid = "ldap_" + suffix
	testutil.Exec(t, db, `UPDATE users SET sso_provider = 'ldap', sso_id = $1 WHERE id = $2`, fmt.Sprintf(ldapBindDNTmpl, uid), id)
	return id, uid
}

func setsCookie(rr *httptest.ResponseRecorder, name string) bool {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return true
		}
	}
	return false
}

func TestLDAPLogin_TOTPRequiresCode(t *testing.T) {
	h, svc, db := newTestRouter(t)
	ctx := context.Background()
	enableLDAP(t, svc, db)
	userID, uid := seedLDAPUser(t, db, testutil.UniqueSuffix(t))
	secret, _, err := svc.TOTP.Generate(uid, "Cloudzilla")
	if err != nil {
		t.Fatalf("TOTP.Generate: %v", err)
	}
	if _, err := svc.TOTP.Enable(ctx, userID, secret, totpNow(t, secret)); err != nil {
		t.Fatalf("TOTP.Enable: %v", err)
	}
	const next = "/settings/security?tab=2fa"

	rr := postForm(h, "/auth/ldap", url.Values{"username": {uid}, "password": {"ldap-password"}, "next": {next}})
	twoFA, err := url.Parse(rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther || err != nil || twoFA.Path != "/auth/2fa" || twoFA.Query().Get("next") != next {
		t.Fatalf("LDAP login: want 303 to /auth/2fa?next=%s, got %d to %q", next, rr.Code, rr.Header().Get("Location"))
	}
	if setsCookie(rr, "cz_token") {
		t.Fatal("LDAP login set cz_token before the TOTP code was checked")
	}
	pending := responseCookie(t, rr, "cz_totp_pending")

	rr = postForm(h, "/auth/2fa/verify", url.Values{"code": {totpNow(t, secret)}, "next": {next}}, pending)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != next {
		t.Fatalf("2FA verify: want 303 to %q, got %d to %q", next, rr.Code, rr.Header().Get("Location"))
	}
	responseCookie(t, rr, "cz_token")
}

func TestLDAPLogin_RecordsLogin(t *testing.T) {
	h, svc, db := newTestRouter(t)
	enableLDAP(t, svc, db)
	userID, uid := seedLDAPUser(t, db, testutil.UniqueSuffix(t))
	const next = "/settings/security"

	rr := postForm(h, "/auth/ldap", url.Values{"username": {uid}, "password": {"ldap-password"}, "next": {next}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != next {
		t.Fatalf("LDAP login: want 303 to %q, got %d to %q", next, rr.Code, rr.Header().Get("Location"))
	}
	responseCookie(t, rr, "cz_token")

	// AuditService.Record writes from a goroutine.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action = $2`, userID, model.AuditActionLogin,
		).Scan(&n)
		if err != nil {
			t.Fatalf("count login events: %v", err)
		}
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("LDAP login recorded no login audit event")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
