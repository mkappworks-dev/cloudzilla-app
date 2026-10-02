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
