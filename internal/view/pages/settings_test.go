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

func TestSettings_NotificationsSectionRendersEmailPrefs(t *testing.T) {
	var sb strings.Builder
	data := view.SettingsData{User: model.User{
		ID:                 42,
		Username:           "alice",
		Email:              "alice@example.com",
		EmailNotifications: true,
		EmailDigest:        model.EmailDigestWeekly,
		NotifyMention:      true,
	}}
	if err := pages.Settings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	start := strings.Index(out, `hx-post="/settings/notifications"`)
	if start < 0 {
		t.Fatal("settings page missing notifications form")
	}
	form := out[start:]
	form = form[:strings.Index(form, "</form>")]

	for _, name := range []string{"email_notifications", "email_digest", "notify_mention", "notify_pr_review"} {
		if !strings.Contains(form, `name="`+name+`"`) {
			t.Errorf("notifications form missing %s", name)
		}
	}
	for _, name := range []string{"notify_issue_assigned", "notify_watched", "notify_weekly_digest"} {
		if strings.Contains(form, name) {
			t.Errorf("notifications form still renders removed pref %s", name)
		}
	}
	if !strings.Contains(form, `<option value="weekly" selected>`) {
		t.Error("digest select does not preselect the saved mode")
	}
	if !strings.Contains(form, `name="notify_mention" checked`) {
		t.Error("notify_mention toggle not checked for a user who has it on")
	}
	if strings.Contains(form, `name="notify_pr_review" checked`) {
		t.Error("notify_pr_review toggle checked for a user who has it off")
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

func TestSettings_DigestSelectLabelsEveryMode(t *testing.T) {
	var sb strings.Builder
	data := view.SettingsData{User: model.User{ID: 42, Username: "alice", EmailDigest: model.EmailDigestImmediate}}
	if err := pages.Settings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()
	for _, mode := range model.EmailDigestModes {
		if strings.Contains(out, `<option value="`+mode+`"></option>`) || strings.Contains(out, `<option value="`+mode+`" selected></option>`) {
			t.Errorf("digest option %q has no label", mode)
		}
		if !strings.Contains(out, `<option value="`+mode+`"`) {
			t.Errorf("digest select missing option %q", mode)
		}
	}
}
