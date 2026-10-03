package pages_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// On/off is a switch on each card that saves by itself, so it lives outside the
// settings form; Use TLS is a setting, a checkbox saved with the form.
func TestSSOSettings_EnableSwitchesAndTLSCheckbox(t *testing.T) {
	var sb strings.Builder
	data := view.SSOSettingsData{
		LDAPConfig: &model.SSOConfig{Provider: "ldap", Enabled: true, Config: map[string]string{
			model.LDAPKeyUseTLS: "true", model.LDAPKeyHost: "ldap.test.invalid", model.LDAPKeyBindDNTmpl: "uid=%s",
		}},
	}
	if err := pages.SSOSettings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	input := func(id string) string {
		return regexp.MustCompile(`<input[^>]*id="` + id + `"[^>]*>`).FindString(out)
	}
	checked := regexp.MustCompile(`\schecked[\s/>]`)
	disabled := regexp.MustCompile(`\sdisabled[\s/>]`)

	if tls := input("ldap_use_tls"); tls == "" || strings.Contains(tls, `role="switch"`) || !checked.MatchString(tls) {
		t.Errorf("Use TLS should be a checked checkbox: %q", tls)
	}
	if sw := input("ldap_enabled"); !strings.Contains(sw, `role="switch"`) || !checked.MatchString(sw) || disabled.MatchString(sw) {
		t.Errorf("LDAP is on and ready, so its switch should be on and usable: %q", sw)
	}
	if sw := input("saml_enabled"); !strings.Contains(sw, `role="switch"`) || checked.MatchString(sw) || !disabled.MatchString(sw) {
		t.Errorf("SAML has nothing saved, so its switch should be off and disabled: %q", sw)
	}

	before := out[:strings.Index(out, `id="ldap_enabled"`)]
	if form := before[strings.LastIndex(before, "<form"):]; !strings.Contains(form, `hx-post="/api/admin/sso/ldap/enabled"`) {
		t.Errorf("LDAP switch is not in its own form:\n%.300s", form)
	}
	assertSwitchesInLabels(t, out)
}

// The hint beside a switch that can't turn on names the empty required fields
// by their labels; the page script keeps it current from the same markers.
func TestSSOSettings_HintNamesWhatToFillIn(t *testing.T) {
	var sb strings.Builder
	data := view.SSOSettingsData{
		LDAPConfig: &model.SSOConfig{Provider: "ldap", Config: map[string]string{model.LDAPKeyHost: "ldap.test.invalid"}},
	}
	if err := pages.SSOSettings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	for provider, want := range map[string]string{
		"ldap": "Fill in Bind DN Template, then save",
		"saml": "Fill in Entity ID, IdP SSO URL, ACS URL and IdP Certificate, then save",
	} {
		hint := regexp.MustCompile(`<span[^>]*id="` + provider + `-switch-needs"[^>]*>([^<]*)</span>`).FindStringSubmatch(out)
		if hint == nil || hint[1] != want {
			t.Errorf("%s hint = %q, want %q", provider, hint, want)
			continue
		}
		if form := `data-sso-needs="sso-` + provider + `-form"`; !strings.Contains(hint[0], form) {
			t.Errorf("%s hint is not tied to its form: %s", provider, hint[0])
		}
	}

	required := regexp.MustCompile(`id="([a-z_]+)"[^>]*data-sso-required="([^"]+)"|data-sso-required="([^"]+)"[^>]*id="([a-z_]+)"`)
	got := map[string]string{}
	for _, m := range required.FindAllStringSubmatch(out, -1) {
		if m[1] != "" {
			got[m[1]] = m[2]
		} else {
			got[m[4]] = m[3]
		}
	}
	for id, label := range map[string]string{
		"ldap_host": "Host", "ldap_bind_dn_tmpl": "Bind DN Template",
		"saml_entity_id": "Entity ID", "saml_sso_url": "IdP SSO URL", "saml_acs_url": "ACS URL", "saml_idp_cert": "IdP Certificate",
	} {
		if got[id] != label {
			t.Errorf("#%s data-sso-required = %q, want %q", id, got[id], label)
		}
	}
	if n := len(regexp.MustCompile(`<button[^>]*data-sso-save`).FindAllString(out, -1)); n != 2 {
		t.Errorf("%d Save buttons marked for the ready ring, want 2", n)
	}
}

// SAML sign-in reads sso_url, so the IdP URL field posts saml_sso_url. Rows
// saved before kept that URL in metadata_url, which the field falls back to.
func TestSSOSettings_SAMLFieldPostsTheSSOURL(t *testing.T) {
	ssoURLField := func(cfg map[string]string) string {
		t.Helper()
		var sb strings.Builder
		data := view.SSOSettingsData{SAMLConfig: &model.SSOConfig{Provider: "saml", Config: cfg}}
		if err := pages.SSOSettings(data).Render(context.Background(), &sb); err != nil {
			t.Fatalf("render: %v", err)
		}
		return regexp.MustCompile(`<input[^>]*name="saml_sso_url"[^>]*>`).FindString(sb.String())
	}
	for _, tc := range []struct {
		cfg  map[string]string
		want string
	}{
		{map[string]string{model.SAMLKeyMetadataURL: "https://idp.test/legacy"}, "https://idp.test/legacy"},
		{map[string]string{model.SAMLKeySSOURL: "https://idp.test/sso", model.SAMLKeyMetadataURL: "https://idp.test/legacy"}, "https://idp.test/sso"},
	} {
		if field := ssoURLField(tc.cfg); !strings.Contains(field, `value="`+tc.want+`"`) {
			t.Errorf("config %v: field = %q, want value %s", tc.cfg, field, tc.want)
		}
	}
}
