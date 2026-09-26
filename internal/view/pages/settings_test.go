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

func renderSettings(t *testing.T, data view.SettingsData) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.Settings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func sectionHTML(t *testing.T, out, id, closeTag string) string {
	t.Helper()
	start := strings.Index(out, `id="`+id+`"`)
	if start < 0 {
		t.Fatalf("settings page missing id=%q", id)
	}
	rest := out[start:]
	end := strings.Index(rest, closeTag)
	if end < 0 {
		t.Fatalf("id=%q has no %s", id, closeTag)
	}
	return rest[:end]
}

func TestSettings_SavedRepliesSectionListsReplies(t *testing.T) {
	out := renderSettings(t, view.SettingsData{
		User:         model.User{ID: 42, Username: "alice"},
		SavedReplies: []model.SavedReply{{ID: 7, Title: "Thanks", Body: "Thanks for the report!"}},
	})
	if !strings.Contains(out, `href="#saved-replies"`) {
		t.Error("settings nav missing saved replies link")
	}
	section := sectionHTML(t, out, "saved-replies", "</section>")
	for _, want := range []string{
		"Thanks for the report!",
		`hx-post="/api/user/replies"`,
		`hx-patch="/api/user/replies/7"`,
		`hx-delete="/api/user/replies/7"`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("saved replies section missing %s", want)
		}
	}
}

func TestSettings_OAuthAppsSectionListsAppsWithoutSecrets(t *testing.T) {
	const secretHash = "$2a$10$storedbcrypthash"
	out := renderSettings(t, view.SettingsData{
		User:                model.User{ID: 42, Username: "alice"},
		OAuthApps:           []model.OAuthApp{{ID: 3, Name: "CI bot", ClientID: "c0ffee", ClientSecret: secretHash}},
		OAuthAuthorizations: []model.OAuthAuthorization{{ID: 9, AppID: 5, Scopes: []string{"repo:read"}}},
	})
	if !strings.Contains(out, `href="#oauth-apps"`) {
		t.Error("settings nav missing OAuth apps link")
	}
	section := sectionHTML(t, out, "oauth-apps", "</section>")
	for _, want := range []string{
		"CI bot",
		"c0ffee",
		"repo:read",
		`hx-post="/api/oauth/apps"`,
		`hx-delete="/api/oauth/apps/3"`,
		`hx-delete="/api/oauth/authorizations/9"`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("OAuth apps section missing %s", want)
		}
	}
	if strings.Contains(out, secretHash) {
		t.Error("settings page renders the stored client secret")
	}
	if strings.Contains(section, "Copy the client secret now") {
		t.Error("settings page shows a client-secret reveal outside the create response")
	}
}

func TestSettings_UnbuiltSectionsAreDisabled(t *testing.T) {
	out := renderSettings(t, view.SettingsData{User: model.User{ID: 42, Username: "alice", Email: "alice@example.com"}})
	for _, s := range []struct{ id, closeTag string }{
		{"export", "</li>"},
		{"sessions", "</section>"},
		{"emails", "</section>"},
	} {
		html := sectionHTML(t, out, s.id, s.closeTag)
		if !strings.Contains(html, "Coming soon") {
			t.Errorf("%s: missing Coming soon label", s.id)
		}
		if strings.Contains(html, "<form") {
			t.Errorf("%s: renders a form", s.id)
		}
		buttons := strings.Count(html, "<button")
		if buttons == 0 {
			t.Errorf("%s: no control rendered", s.id)
		}
		if disabled := strings.Count(html, "<button type=\"button\" disabled"); disabled != buttons {
			t.Errorf("%s: %d of %d buttons are disabled", s.id, disabled, buttons)
		}
	}
	for _, leak := range []string{"/settings/export", "Export requested"} {
		if strings.Contains(out, leak) {
			t.Errorf("settings page still references %q", leak)
		}
	}
}
