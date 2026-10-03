package pages_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

var (
	formTag      = regexp.MustCompile(`(?s)<form[^>]*>.*?</form>`)
	passwordName = regexp.MustCompile(`name="password"`)
)

// The admin pages ask for the password in the identity dialog, not inline, so
// every form that needs it must opt in and none may carry its own field.
func TestAdminPages_ConfirmInTheIdentityDialog(t *testing.T) {
	confirm := components.ConfirmFactors{Password: true}
	cases := map[string]struct {
		page      templ.Component
		confirmed []string
	}{
		"settings": {
			page: pages.AdminSettings(view.AdminSettingsData{
				Settings: []model.SiteSetting{{Key: "allow_login", Value: "true"}, {Key: "allow_registration", Value: "false"}},
				Confirm:  confirm,
			}),
			confirmed: []string{`hx-post="/api/admin/settings"`, `hx-post="/api/admin/invitations"`, `hx-post="/api/admin/users/verify-email"`},
		},
		"sso": {
			page: pages.SSOSettings(view.SSOSettingsData{
				LDAPConfig: &model.SSOConfig{Provider: "ldap", Config: map[string]string{model.LDAPKeyHost: "h", model.LDAPKeyBindDNTmpl: "uid=%s"}},
				SAMLConfig: &model.SSOConfig{Provider: "saml", Enabled: true},
				Confirm:    confirm,
			}),
			confirmed: []string{
				`id="sso-ldap-form"`, `id="sso-saml-form"`,
				`hx-post="/api/admin/sso/ldap/enabled"`, `hx-post="/api/admin/sso/saml/enabled"`,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			if err := tc.page.Render(context.Background(), &sb); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := sb.String()
			if !strings.Contains(out, `<dialog id="cz-identity-dialog"`) {
				t.Error("no identity dialog")
			}
			if strings.Contains(out, "hx-include") {
				t.Error("a form still pulls the password from an inline field")
			}
			dialog := out[strings.Index(out, `<dialog id="cz-identity-dialog"`):]
			dialog, _, _ = strings.Cut(dialog, "</dialog>")
			for _, form := range formTag.FindAllString(out, -1) {
				if passwordName.MatchString(form) && !strings.Contains(dialog, form) {
					t.Errorf("form outside the dialog has its own password field:\n%s", form)
				}
			}
			for _, marker := range tc.confirmed {
				tag := regexp.MustCompile(`<form(?:[^>"]|"[^"]*")*`+regexp.QuoteMeta(marker)+`(?:[^>"]|"[^"]*")*>`).FindAllString(out, -1)
				if len(tag) == 0 {
					t.Errorf("no form with %s", marker)
				}
				for _, f := range tag {
					if !strings.Contains(f, "data-confirm-identity=") {
						t.Errorf("form does not ask for confirmation: %s", f)
					}
				}
			}
		})
	}
}

func TestAdminSettings_FlagsAreSwitches(t *testing.T) {
	var sb strings.Builder
	data := view.AdminSettingsData{
		Settings: []model.SiteSetting{{Key: "allow_login", Value: "true"}, {Key: "allow_registration", Value: "false"}},
	}
	if err := pages.AdminSettings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	for key, on := range map[string]bool{"allow_login": true, "allow_registration": false} {
		sw := regexp.MustCompile(`<input[^>]*id="setting-` + key + `"[^>]*>`).FindString(out)
		if !strings.Contains(sw, `role="switch"`) {
			t.Errorf("%s is not a switch: %q", key, sw)
			continue
		}
		if got := regexp.MustCompile(`\schecked[\s/>]`).MatchString(sw); got != on {
			t.Errorf("%s checked = %v, want %v", key, got, on)
		}
	}
	if strings.Contains(out, ">Enable<") || strings.Contains(out, ">Disable<") {
		t.Error("Enable/Disable buttons are still rendered")
	}
	assertSwitchesInLabels(t, out)
}

// The switch's input is sr-only, so its visible track takes clicks only
// through an enclosing <label>.
func assertSwitchesInLabels(t *testing.T, out string) {
	t.Helper()
	switches := regexp.MustCompile(`role="switch"`).FindAllStringIndex(out, -1)
	if len(switches) == 0 {
		t.Fatal("no switches rendered")
	}
	for _, sw := range switches {
		before := out[:sw[0]]
		if strings.LastIndex(before, "<label") <= strings.LastIndex(before, "</label>") {
			t.Errorf("switch outside a <label>: …%s", out[max(0, sw[0]-160):sw[1]])
		}
	}
}
