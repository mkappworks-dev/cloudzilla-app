package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// The switch in a provider's card turns it on only once the settings sign-in
// needs are saved, and saving settings never flips it.
func TestSSOSwitch_OnlyWithWhatSignInNeeds(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	ctx := context.Background()
	prior, err := svc.SSO.GetConfig(ctx, "ldap")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if prior == nil {
			testutil.Exec(t, db, `DELETE FROM sso_configs WHERE provider = 'ldap'`)
		} else if err := svc.SSO.SetConfig(ctx, "ldap", prior.Config, prior.Enabled); err != nil {
			t.Errorf("restore ldap config: %v", err)
		}
	})
	testutil.Exec(t, db, `DELETE FROM sso_configs WHERE provider = 'ldap'`)

	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, "password1")
	admin := superadminJWT(t, adminID, "testadmin_"+suffix)

	turn := func(on bool) *httptest.ResponseRecorder {
		form := withPassword(url.Values{"enabled": {strconv.FormatBool(on)}}, "password1")
		return serve(h, htmxRequest(browserRequest(http.MethodPost, "/api/admin/sso/ldap/enabled", admin, form)))
	}
	save := func(host, bindDNTmpl string) *httptest.ResponseRecorder {
		form := withPassword(url.Values{"provider": {"ldap"}, "ldap_host": {host}, "ldap_bind_dn_tmpl": {bindDNTmpl}}, "password1")
		return serve(h, browserRequest(http.MethodPost, "/admin/sso", admin, form))
	}
	state := func() (on bool, host string) {
		t.Helper()
		cfg, err := svc.SSO.GetConfig(ctx, "ldap")
		if err != nil {
			t.Fatal(err)
		}
		if cfg == nil {
			return false, ""
		}
		return cfg.Enabled, cfg.Config[model.LDAPKeyHost]
	}

	if rr := turn(true); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("on with nothing saved: got %d %s", rr.Code, rr.Body)
	}
	save("ldap.test.invalid", "")
	if rr := turn(true); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("on without a bind DN template: got %d %s", rr.Code, rr.Body)
	}
	if on, _ := state(); on {
		t.Fatal("LDAP is on without the settings sign-in needs")
	}

	save("ldap.test.invalid", "uid=%s,dc=test")
	if rr := turn(true); rr.Code != http.StatusOK || rr.Header().Get("HX-Refresh") != "true" {
		t.Errorf("on: got %d, HX-Refresh %q, %s", rr.Code, rr.Header().Get("HX-Refresh"), rr.Body)
	}
	save("ldap2.test.invalid", "uid=%s,dc=test")
	if on, host := state(); !on || host != "ldap2.test.invalid" {
		t.Errorf("after saving while on: on = %v, host = %q", on, host)
	}

	if rr := save("", "uid=%s,dc=test"); !strings.Contains(rr.Body.String(), "Turn LDAP off") {
		t.Errorf("clearing the host while on was not refused:\n%.400s", rr.Body)
	}
	if on, host := state(); !on || host != "ldap2.test.invalid" {
		t.Errorf("after the refused save: on = %v, host = %q", on, host)
	}

	turn(false)
	if on, host := state(); on || host != "ldap2.test.invalid" {
		t.Errorf("off: on = %v, host = %q; want off with the settings kept", on, host)
	}
}
