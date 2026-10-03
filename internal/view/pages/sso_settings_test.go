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

func TestSSOSettings_FlagsAreSwitchesReflectingConfig(t *testing.T) {
	var sb strings.Builder
	data := view.SSOSettingsData{
		LDAPConfig: &model.SSOConfig{Enabled: true, Config: map[string]string{"use_tls": "true"}},
	}
	if err := pages.SSOSettings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	for id, wantChecked := range map[string]bool{
		"ldap_use_tls": true,
		"ldap_enabled": true,
		"saml_enabled": false,
	} {
		input := regexp.MustCompile(`<input[^>]*id="` + id + `"[^>]*>`).FindString(out)
		if input == "" {
			t.Errorf("no input #%s", id)
			continue
		}
		if !strings.Contains(input, `role="switch"`) {
			t.Errorf("#%s is not a switch: %s", id, input)
		}
		if got := regexp.MustCompile(`\schecked[\s/>]`).MatchString(input); got != wantChecked {
			t.Errorf("#%s checked = %v, want %v", id, got, wantChecked)
		}
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
