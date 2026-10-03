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
