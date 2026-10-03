package pages_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

var (
	passwordField = regexp.MustCompile(`<input[^>]*type="password"[^>]*>\s*<button[^>]*>`)
	revealToggle  = regexp.MustCompile(`<button[^>]*data-password-reveal[^>]*>$`)
	classAttr     = regexp.MustCompile(`class="([^"]*)"`)
)

// components.PasswordInput defaults to the compact settings field; on the
// sign-in and sign-up cards it must look like the components.Input beside it.
func TestAuthPasswordFields_HaveRevealToggleAndMatchInput(t *testing.T) {
	for name, tc := range map[string]struct {
		page   templ.Component
		fields int
	}{
		"Login":            {pages.Login(view.LoginData{LDAPEnabled: true}), 2},
		"Register":         {pages.Register(view.RegisterData{}), 1},
		"RegisterComplete": {pages.RegisterComplete(view.RegisterCompleteData{Signup: &model.SignupToken{}}), 1},
		"Invite":           {pages.Invite(view.InviteData{Invitation: &model.Invitation{}}), 1},
		"Setup":            {pages.Setup(view.SetupData{}), 1},
	} {
		t.Run(name, func(t *testing.T) {
			fields := passwordField.FindAllString(render(t, tc.page), -1)
			if len(fields) != tc.fields {
				t.Fatalf("found %d password fields with a following button, want %d", len(fields), tc.fields)
			}
			for _, f := range fields {
				if !revealToggle.MatchString(f) {
					t.Errorf("password field lacks a show/hide toggle:\n%s", f)
				}
				input, _, _ := strings.Cut(f, "<button")
				m := classAttr.FindStringSubmatch(input)
				if m == nil {
					t.Fatalf("password input has no class:\n%s", input)
				}
				classes := " " + m[1] + " "
				for _, want := range []string{"bg-transparent", "text-sm", "shadow-xs"} {
					if !strings.Contains(classes, " "+want+" ") {
						t.Errorf("password input lacks Input's %s: %s", want, m[1])
					}
				}
				for _, dropped := range []string{"bg-background", "text-[13px]", "focus:outline-hidden"} {
					if strings.Contains(classes, " "+dropped+" ") {
						t.Errorf("password input keeps the settings field's %s: %s", dropped, m[1])
					}
				}
			}
		})
	}
}
