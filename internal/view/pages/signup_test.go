package pages

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

func renderPage(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestRegisterEmail_OnlyAsksForEmail(t *testing.T) {
	body := renderPage(t, RegisterEmail(view.RegisterEmailData{Email: "a@test.invalid", Error: "Enter a valid email address"}))
	if !strings.Contains(body, `name="email"`) || strings.Contains(body, `name="password"`) {
		t.Errorf("want an email-only form:\n%s", body)
	}
	if !strings.Contains(body, "Enter a valid email address") || !strings.Contains(body, `value="a@test.invalid"`) {
		t.Errorf("want the error and the submitted email kept:\n%s", body)
	}
}

func TestRegisterCheckInbox_SaysCheckYourInbox(t *testing.T) {
	if body := renderPage(t, RegisterCheckInbox(view.RegisterCheckInboxData{})); !strings.Contains(body, "Check your inbox") {
		t.Errorf("want the check-inbox heading:\n%s", body)
	}
}

func TestRegisterComplete_UsableLink_ShowsEmailReadOnlyForm(t *testing.T) {
	body := renderPage(t, RegisterComplete(view.RegisterCompleteData{Signup: &model.SignupToken{Email: "a@test.invalid"}, Username: "alice"}))
	for _, want := range []string{`value="a@test.invalid"`, "readonly", `name="username"`, `value="alice"`, `name="password"`} {
		if !strings.Contains(body, want) {
			t.Errorf("completion form must contain %s:\n%s", want, body)
		}
	}
}

func TestRegisterComplete_NoSignup_ShowsInvalidLinkWithoutForm(t *testing.T) {
	body := renderPage(t, RegisterComplete(view.RegisterCompleteData{}))
	if !strings.Contains(body, "This link is no longer valid") || strings.Contains(body, `name="username"`) {
		t.Errorf("want the invalid-link page without a form:\n%s", body)
	}
}
