package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const ssoAdminPassword = "sso-admin-pw"

type ssoEnv struct {
	h       http.Handler
	svc     *service.Services
	db      *sql.DB
	adminID int64
	admin   string
	userTok string
}

// newSSOEnv restores both providers' configs when the test ends: sso_configs is
// instance-wide and other tests read it.
func newSSOEnv(t *testing.T) ssoEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	ctx := context.Background()
	for _, p := range []string{"ldap", "saml"} {
		prior, err := svc.SSO.GetConfig(ctx, p)
		if err != nil {
			t.Fatalf("GetConfig(%s): %v", p, err)
		}
		testutil.Exec(t, db, `DELETE FROM sso_configs WHERE provider = $1`, p)
		t.Cleanup(func() {
			testutil.Exec(t, db, `DELETE FROM sso_configs WHERE provider = $1`, p)
			if prior != nil {
				if err := svc.SSO.SetConfig(ctx, p, prior.Config, prior.Enabled); err != nil {
					t.Errorf("restore %s config: %v", p, err)
				}
			}
		})
	}
	sfx := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, sfx)
	testutil.SetPassword(t, db, adminID, ssoAdminPassword)
	userSfx := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, userSfx)
	return ssoEnv{h: h, svc: svc, db: db, adminID: adminID, admin: "testadmin_" + sfx, userTok: makeJWT(t, userID, "testuser_"+userSfx)}
}

func (e ssoEnv) adminTok(t *testing.T) string { return superadminJWT(t, e.adminID, e.admin) }

func (e ssoEnv) send(req *http.Request, htmx bool) *httptest.ResponseRecorder {
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func (e ssoEnv) setConfig(t *testing.T, provider string, cfg map[string]string, enabled bool) {
	t.Helper()
	if err := e.svc.SSO.SetConfig(context.Background(), provider, cfg, enabled); err != nil {
		t.Fatalf("SetConfig(%s): %v", provider, err)
	}
}

func (e ssoEnv) config(t *testing.T, provider string) *model.SSOConfig {
	t.Helper()
	cfg, err := e.svc.SSO.GetConfig(context.Background(), provider)
	if err != nil {
		t.Fatalf("GetConfig(%s): %v", provider, err)
	}
	return cfg
}

var readyLDAP = map[string]string{model.LDAPKeyHost: "ldap.invalid", model.LDAPKeyBindDNTmpl: "uid=%s,dc=example"}

var readySAML = map[string]string{
	model.SAMLKeyEntityID: "https://sp.invalid/meta",
	model.SAMLKeySSOURL:   "https://idp.invalid/sso",
	model.SAMLKeyACSURL:   "https://sp.invalid/auth/saml/callback",
	model.SAMLKeyCert:     "CERT",
}

func ldapForm(extra url.Values) url.Values {
	f := url.Values{
		"provider": {"ldap"}, "password": {ssoAdminPassword},
		"ldap_host": {"ldap.invalid"}, "ldap_port": {"389"}, "ldap_base_dn": {"dc=example"},
		"ldap_bind_dn_tmpl": {"uid=%s,dc=example"}, "ldap_use_tls": {"false"},
	}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func TestSSOSettingsPage_SuperadminOnly(t *testing.T) {
	e := newSSOEnv(t)
	e.setConfig(t, "ldap", readyLDAP, false)

	rr := e.send(browserRequest("GET", "/admin/sso", e.adminTok(t), nil), false)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "ldap.invalid") {
		t.Fatalf("superadmin got %d: %.300s", rr.Code, rr.Body.String())
	}

	if rr := e.send(browserRequest("GET", "/admin/sso", e.userTok, nil), false); rr.Code != http.StatusForbidden {
		t.Errorf("regular user got %d, want 403", rr.Code)
	}
	if rr := e.send(browserRequest("GET", "/admin/sso", "", nil), false); rr.Code == http.StatusOK {
		t.Errorf("anonymous got the settings page")
	}
}

func TestSSOSave_StoresSettingsAndKeepsState(t *testing.T) {
	e := newSSOEnv(t)

	rr := e.send(browserRequest("POST", "/admin/sso", e.adminTok(t), ldapForm(nil)), false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "SSO configuration saved.")
	cfg := e.config(t, "ldap")
	if cfg == nil || cfg.Enabled || cfg.Config[model.LDAPKeyHost] != "ldap.invalid" || cfg.Config[model.LDAPKeyBindDNTmpl] != "uid=%s,dc=example" {
		t.Errorf("stored ldap config = %+v", cfg)
	}

	saml := url.Values{
		"provider": {"saml"}, "password": {ssoAdminPassword},
		"saml_entity_id": {readySAML[model.SAMLKeyEntityID]}, "saml_sso_url": {readySAML[model.SAMLKeySSOURL]},
		"saml_acs_url": {readySAML[model.SAMLKeyACSURL]}, "saml_idp_cert": {"CERT"},
	}
	rr = e.send(browserRequest("POST", "/admin/sso", e.adminTok(t), saml), true)
	wantStatus(t, rr, http.StatusNoContent)
	if got := rr.Header().Get("HX-Redirect"); got != "/admin/sso" {
		t.Errorf("HX-Redirect = %q", got)
	}
	if cfg := e.config(t, "saml"); cfg == nil || cfg.Config[model.SAMLKeySSOURL] != readySAML[model.SAMLKeySSOURL] {
		t.Errorf("stored saml config = %+v", cfg)
	}
}

func TestSSOSave_Refusals(t *testing.T) {
	e := newSSOEnv(t)
	tok := e.adminTok(t)

	rr := e.send(browserRequest("POST", "/admin/sso", e.userTok, ldapForm(nil)), false)
	wantStatus(t, rr, http.StatusForbidden)

	rr = e.send(browserRequest("POST", "/admin/sso", tok, ldapForm(url.Values{"provider": {"radius"}})), false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Unknown provider: radius")
	rr = e.send(browserRequest("POST", "/admin/sso", tok, ldapForm(url.Values{"provider": {"radius"}})), true)
	wantStatus(t, rr, http.StatusBadRequest)

	rr = e.send(browserRequest("POST", "/admin/sso", tok, ldapForm(url.Values{"password": {"wrong"}})), false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "incorrect")
	rr = e.send(browserRequest("POST", "/admin/sso", tok, ldapForm(url.Values{"password": {"wrong"}})), true)
	if got := rr.Header().Get("HX-Retarget"); got != "#sso-ldap-form-error" {
		t.Errorf("HX-Retarget = %q", got)
	}
	bodyHas(t, rr, "incorrect")

	if cfg := e.config(t, "ldap"); cfg != nil {
		t.Errorf("a refused save stored %+v", cfg)
	}
}

func TestSSOSave_ClearingASettingWhileOnIsRefused(t *testing.T) {
	e := newSSOEnv(t)
	e.setConfig(t, "ldap", readyLDAP, true)

	form := ldapForm(url.Values{"ldap_host": {""}})
	rr := e.send(browserRequest("POST", "/admin/sso", e.adminTok(t), form), false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Turn LDAP off before clearing it.")
	rr = e.send(browserRequest("POST", "/admin/sso", e.adminTok(t), form), true)
	if got := rr.Header().Get("HX-Retarget"); got != "#sso-ldap-form-error" {
		t.Errorf("HX-Retarget = %q", got)
	}
	if cfg := e.config(t, "ldap"); cfg.Config[model.LDAPKeyHost] != "ldap.invalid" || !cfg.Enabled {
		t.Errorf("config changed: %+v", cfg)
	}
}

func TestSSOEnabled_Switch(t *testing.T) {
	e := newSSOEnv(t)
	tok := e.adminTok(t)
	post := func(provider, token, enabled, password string) *http.Request {
		return browserRequest("POST", "/api/admin/sso/"+provider+"/enabled", token, url.Values{"enabled": {enabled}, "password": {password}})
	}

	wantStatus(t, e.send(post("ldap", e.userTok, "true", ssoAdminPassword), false), http.StatusForbidden)
	wantStatus(t, e.send(post("radius", tok, "true", ssoAdminPassword), false), http.StatusNotFound)
	wantStatus(t, e.send(post("ldap", tok, "true", "wrong"), false), http.StatusForbidden)
	wantStatus(t, e.send(post("ldap", tok, "true", ssoAdminPassword), false), http.StatusUnprocessableEntity)
	if e.config(t, "ldap") != nil {
		t.Fatal("refused requests created a config")
	}

	e.setConfig(t, "ldap", readyLDAP, false)
	rr := e.send(post("ldap", tok, "true", ssoAdminPassword), false)
	wantStatus(t, rr, http.StatusSeeOther)
	if got := rr.Header().Get("Location"); got != "/admin/sso" {
		t.Errorf("Location = %q", got)
	}
	if !e.config(t, "ldap").Enabled {
		t.Fatal("ldap not turned on")
	}

	rr = e.send(post("ldap", tok, "false", ssoAdminPassword), true)
	wantStatus(t, rr, http.StatusOK)
	if rr.Header().Get("HX-Refresh") != "true" {
		t.Error("no HX-Refresh")
	}
	if e.config(t, "ldap").Enabled {
		t.Error("ldap not turned off")
	}
}

func TestLDAPLogin_RefusesBeforeAnySession(t *testing.T) {
	e := newSSOEnv(t)
	login := func(form url.Values) *httptest.ResponseRecorder {
		return e.send(browserRequest("POST", "/auth/ldap", "", form), false)
	}

	rr := login(url.Values{"username": {"someone"}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Username and password are required.")

	rr = login(url.Values{"username": {"someone"}, "password": {"pw"}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "LDAP authentication failed.")
	if setsCookie(rr, "cz_token") {
		t.Error("a failed LDAP login set a session cookie")
	}
}

func TestSAMLInitiate(t *testing.T) {
	e := newSSOEnv(t)
	get := func() *httptest.ResponseRecorder {
		return e.send(httptest.NewRequest("GET", "/auth/saml?next=/dashboard", nil), false)
	}

	wantStatus(t, get(), http.StatusServiceUnavailable)

	e.setConfig(t, "saml", readySAML, false)
	wantStatus(t, get(), http.StatusServiceUnavailable)

	e.setConfig(t, "saml", readySAML, true)
	rr := get()
	wantStatus(t, rr, http.StatusFound)
	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil || loc.Host != "idp.invalid" || loc.Query().Get("SAMLRequest") == "" || loc.Query().Get("RelayState") != "/dashboard" {
		t.Errorf("Location = %q (%v)", rr.Header().Get("Location"), err)
	}
}

func TestSAMLMetadata(t *testing.T) {
	e := newSSOEnv(t)
	get := func() *httptest.ResponseRecorder {
		return e.send(httptest.NewRequest("GET", "/auth/saml/metadata", nil), false)
	}

	wantStatus(t, get(), http.StatusServiceUnavailable)

	e.setConfig(t, "saml", readySAML, true)
	rr := get()
	wantStatus(t, rr, http.StatusOK)
	if ct := rr.Header().Get("Content-Type"); ct != "application/xml" {
		t.Errorf("Content-Type = %q", ct)
	}
	bodyHas(t, rr, readySAML[model.SAMLKeyACSURL])
	bodyHas(t, rr, readySAML[model.SAMLKeyEntityID])
}

func TestSAMLCallback_Refusals(t *testing.T) {
	e := newSSOEnv(t)
	post := func(form url.Values) *httptest.ResponseRecorder {
		return e.send(browserRequest("POST", "/auth/saml/callback", "", form), false)
	}

	wantStatus(t, post(url.Values{}), http.StatusBadRequest)

	rr := post(url.Values{"SAMLResponse": {"not-base64-xml"}, "RelayState": {"/next"}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "SAML authentication failed.")
	if setsCookie(rr, "cz_token") {
		t.Error("a failed SAML callback set a session cookie")
	}

	rr = post(url.Values{"SAMLResponse": {"not-base64-xml"}, "RelayState": {"reauth:state"}})
	wantStatus(t, rr, http.StatusSeeOther)
	if setsCookie(rr, "cz_token") {
		t.Error("a failed SAML re-authentication set a session cookie")
	}
	var failedCookie bool
	for _, c := range rr.Result().Cookies() {
		failedCookie = failedCookie || (c.Value == "1" && c.MaxAge == 60)
	}
	if !failedCookie {
		t.Error("a failed SAML re-authentication left no failure cookie")
	}
}
