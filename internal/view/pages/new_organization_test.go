package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func renderNewOrganization(t *testing.T, data view.NewOrganizationData) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.NewOrganization(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// The form must expose the name input, TOS checkbox, submit button, and the
// decorative Enterprise/Team options must be disabled.
func TestNewOrganization_RendersForm(t *testing.T) {
	data := view.NewOrganizationData{
		BasePage: view.BasePage{
			CurrentUser: &middleware.Claims{UserID: 1, Username: "octocat"},
		},
	}
	out := renderNewOrganization(t, data)

	for _, want := range []string{
		`name="name"`,
		`name="accept_tos"`,
		"Create organization",
		"Terms of Service",
		"octocat",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("new-organization form missing %q\n--- output ---\n%s", want, out)
		}
	}

	// Both decorative alternative options must be disabled.
	if strings.Count(out, "disabled") < 2 {
		t.Errorf("expected Enterprise and Team radios to be disabled\n--- output ---\n%s", out)
	}
}

// An error must render in a role="alert" element, and the submitted Name and
// Description values must be preserved on the re-render.
func TestNewOrganization_ShowsError(t *testing.T) {
	data := view.NewOrganizationData{
		BasePage: view.BasePage{
			CurrentUser: &middleware.Claims{UserID: 1, Username: "octocat"},
		},
		Error:       "boom",
		Name:        "acme-corp",
		Description: "we make everything",
	}
	out := renderNewOrganization(t, data)

	if !strings.Contains(out, `role="alert"`) || !strings.Contains(out, "boom") {
		t.Errorf("expected error banner with \"boom\"\n--- output ---\n%s", out)
	}
	for _, want := range []string{"acme-corp", "we make everything"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected preserved value %q\n--- output ---\n%s", want, out)
		}
	}
}
