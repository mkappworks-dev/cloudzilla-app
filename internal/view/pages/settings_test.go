package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestSettings_ProfileFormDoesNotSubmitUsername(t *testing.T) {
	var sb strings.Builder
	data := view.SettingsData{User: model.User{ID: 42, Username: "alice", Email: "alice@example.com"}}
	if err := pages.Settings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	start := strings.Index(out, `action="/settings/profile"`)
	if start < 0 {
		t.Fatal("settings page missing profile form")
	}
	form := out[start:]
	form = form[:strings.Index(form, "</form>")]

	if strings.Contains(form, `name="username"`) {
		t.Error("profile form submits a username field")
	}
	if !strings.Contains(form, `value="alice"`) {
		t.Error("profile form does not display the current username")
	}
}

func TestSettings_ProfileErrorShowsSpecificMessage(t *testing.T) {
	for code, want := range map[string]string{
		"invalid_email": "Enter a valid email address.",
		"email_taken":   "That email is already in use.",
	} {
		var sb strings.Builder
		data := view.SettingsData{User: model.User{ID: 42, Username: "alice"}, ProfileError: code}
		if err := pages.Settings(data).Render(context.Background(), &sb); err != nil {
			t.Fatalf("render: %v", err)
		}
		if out := sb.String(); !strings.Contains(out, want) || strings.Contains(out, "Something went wrong.") {
			t.Errorf("profile_error=%s: want %q, not the generic message", code, want)
		}
	}
}
