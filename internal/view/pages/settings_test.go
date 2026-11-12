package pages_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestSettings_ProfileFormDoesNotSubmitUsername(t *testing.T) {
	var sb strings.Builder
	data := view.SettingsData{User: model.User{ID: 42, Username: "alice", Email: "alice@example.com"}}
	if err := pages.Settings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	start := strings.Index(out, `hx-post="/settings/profile"`)
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
	for _, u := range []model.User{
		{EmailNotifications: true, EmailDigest: model.EmailDigestWeekly, NotifyMention: true},
		{EmailNotifications: false, EmailDigest: model.EmailDigestDaily, NotifyPRReview: true},
	} {
		u.ID, u.Username, u.Email = 42, "alice", "alice@example.com"
		out := renderSettings(t, view.SettingsData{User: u})

		start := strings.Index(out, `hx-post="/settings/notifications"`)
		if start < 0 {
			t.Fatal("settings page missing notifications form")
		}
		form := out[start:]
		form = form[:strings.Index(form, "</form>")]

		for _, name := range []string{"notify_issue_assigned", "notify_watched", "notify_weekly_digest"} {
			if strings.Contains(form, name) {
				t.Errorf("notifications form still renders removed pref %s", name)
			}
		}
		if !strings.Contains(form, `<option value="`+u.EmailDigest+`" selected>`) {
			t.Errorf("digest select does not preselect the saved mode %q", u.EmailDigest)
		}
		for name, on := range map[string]bool{
			"email_notifications": u.EmailNotifications,
			"notify_pr_review":    u.NotifyPRReview,
			"notify_mention":      u.NotifyMention,
		} {
			checked, found := checkboxChecked(form, name)
			if !found {
				t.Errorf("notifications form missing %s", name)
			} else if checked != on {
				t.Errorf("%s: checked = %v, want %v", name, checked, on)
			}
		}
	}
}

func checkboxChecked(html, name string) (checked, found bool) {
	i := strings.Index(html, `name="`+name+`"`)
	if i < 0 {
		return false, false
	}
	tag := html[strings.LastIndex(html[:i], "<"):]
	tag = tag[:strings.Index(tag, ">")]
	return strings.Contains(tag, " checked"), true
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

func TestSettings_TokenFormOffersEveryScopeAndRequiresOne(t *testing.T) {
	out := renderSettings(t, view.SettingsData{User: model.User{ID: 42, Username: "alice"}})
	start := strings.Index(out, `hx-post="/api/user/tokens"`)
	if start < 0 {
		t.Fatal("settings page missing token form")
	}
	form := out[start:]
	form = form[:strings.Index(form, "</form>")]

	for _, s := range model.Scopes {
		if !strings.Contains(form, `value="`+s+`"`) || !strings.Contains(form, model.ScopeDescription(s)) {
			t.Errorf("token form does not offer %s with its description", s)
		}
	}
	if !regexp.MustCompile(`<button type="submit"[^>]*:disabled="picked === 0"`).MatchString(form) {
		t.Error("token form can be submitted before a scope is chosen")
	}
}

func TestSettings_ExportRowFollowsTheLatestExport(t *testing.T) {
	ready := time.Date(2026, 11, 12, 9, 14, 0, 0, time.UTC)
	expires := ready.Add(7 * 24 * time.Hour)
	for _, tc := range []struct {
		name   string
		export *model.AccountExport
		want   []string
		absent []string
	}{
		{"none", nil, []string{"Request export", `action="/settings/export"`}, []string{"Download zip", ">Preparing<", ">Failed<"}},
		{"requested", &model.AccountExport{ID: 7, Status: model.ExportRequested}, []string{">Preparing<", "We'll email you"}, []string{"Request export", "Download zip"}},
		{"running", &model.AccountExport{ID: 7, Status: model.ExportRunning}, []string{">Preparing<"}, []string{"Request export", "Download zip"}},
		{"ready", &model.AccountExport{ID: 7, Status: model.ExportReady, ReadyAt: &ready, ExpiresAt: &expires},
			[]string{">Ready<", `action="/settings/export/7/download"`, "Download zip", "Nov 19, 2026"}, []string{"Request export", "Request again"}},
		{"failed", &model.AccountExport{ID: 7, Status: model.ExportFailed, Error: "The export failed. Request a new one."},
			[]string{">Failed<", "Request again", "The export failed. Request a new one."}, []string{"Download zip"}},
	} {
		html := sectionHTML(t, renderSettings(t, view.SettingsData{
			User: model.User{ID: 42, Username: "alice"}, Export: tc.export, ExportEmails: true,
			Confirm: components.ConfirmFactors{Password: true},
		}), "export", "</li>")
		for _, w := range tc.want {
			if !strings.Contains(html, w) {
				t.Errorf("%s: missing %q in\n%s", tc.name, w, html)
			}
		}
		for _, a := range tc.absent {
			if strings.Contains(html, a) {
				t.Errorf("%s: unexpected %q", tc.name, a)
			}
		}
	}
	if out := renderSettings(t, view.SettingsData{User: model.User{ID: 42, Username: "alice"}}); strings.Contains(out, "Coming soon") {
		t.Error("the settings page still has a Coming soon label")
	}
}

func TestSettings_EmailSectionPrivacyToggle(t *testing.T) {
	for _, keep := range []bool{true, false} {
		out := renderSettings(t, view.SettingsData{
			User:         model.User{ID: 42, Username: "alice", Email: "alice@example.com", KeepEmailPrivate: keep},
			NoreplyEmail: "42+alice@users.noreply.example.com",
		})
		html := sectionHTML(t, out, "email", "</section>")
		if !strings.Contains(html, `hx-post="/settings/email"`) {
			t.Errorf("keep=%v: email form does not post to /settings/email", keep)
		}
		if !strings.Contains(html, "42+alice@users.noreply.example.com") {
			t.Errorf("keep=%v: noreply address not shown", keep)
		}
		checked := regexp.MustCompile(`<input[^>]*name="keep_email_private"[^>]*checked`).MatchString(html)
		if checked != keep {
			t.Errorf("keep=%v: toggle checked = %v", keep, checked)
		}
	}
}

func TestSettings_EmailSectionAdditionalAddresses(t *testing.T) {
	user := model.User{ID: 42, Username: "alice", Email: "alice@example.com"}
	extras := []model.UserEmail{
		{ID: 7, UserID: 42, Email: "work@example.com", VerifiedAt: &time.Time{}},
		{ID: 8, UserID: 42, Email: "old@example.com"},
	}
	t.Run("with SMTP", func(t *testing.T) {
		html := sectionHTML(t, renderSettings(t, view.SettingsData{User: user, EmailVerificationAvailable: true, AdditionalEmails: extras}), "email", "</section>")
		for _, want := range []string{">Add email<", `hx-post="/settings/emails"`, "work@example.com", "old@example.com",
			`hx-post="/settings/emails/7/remove"`, `hx-post="/settings/emails/8/remove"`} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q", want)
			}
		}
		if got := strings.Count(html, ">Remove<"); got != 2 {
			t.Errorf("%d Remove controls, want one per additional address and none on the primary", got)
		}
		if strings.Count(html, ">Verified<") != 1 || strings.Count(html, ">Unverified<") != 2 {
			t.Errorf("chips: %d Verified, %d Unverified; want 1 and 2 (the primary is unverified)", strings.Count(html, ">Verified<"), strings.Count(html, ">Unverified<"))
		}
	})
	t.Run("without SMTP", func(t *testing.T) {
		html := sectionHTML(t, renderSettings(t, view.SettingsData{User: user, AdditionalEmails: extras}), "email", "</section>")
		if strings.Contains(html, ">Add email<") || strings.Contains(html, `hx-post="/settings/emails"`) {
			t.Error("Add email is offered without outgoing email")
		}
		if !strings.Contains(html, "Adding another address needs outgoing email") {
			t.Error("the missing outgoing email is not explained")
		}
		if !strings.Contains(html, "work@example.com") || !strings.Contains(html, `hx-post="/settings/emails/7/remove"`) {
			t.Error("existing addresses can no longer be removed")
		}
	})
}

func TestSettings_SoleOrgOwnerDeleteRefusalIsExplained(t *testing.T) {
	out := renderSettings(t, view.SettingsData{
		User:         model.User{ID: 42, Username: "alice", Email: "alice@example.com"},
		ProfileError: "sole_org_owner",
	})
	if !strings.Contains(out, "You are the only owner of an organization. Add another owner or delete the organization first.") {
		t.Error("sole_org_owner renders without its message")
	}
}

func TestSettings_LastSuperadminDeleteRefusalIsExplained(t *testing.T) {
	out := renderSettings(t, view.SettingsData{
		User:         model.User{ID: 42, Username: "alice", Email: "alice@example.com"},
		ProfileError: "last_superadmin",
	})
	if !strings.Contains(out, "You are the only active superadmin.") {
		t.Error("last_superadmin renders without its message")
	}
}

func TestSettings_PasswordChangeMatchesTheAccount(t *testing.T) {
	user := model.User{ID: 42, Username: "alice", Email: "alice@example.com"}
	for _, tt := range []struct {
		name                  string
		hasPassword, withTOTP bool
	}{
		{"password", true, false},
		{"password and 2FA", true, true},
		{"Google only", false, false},
	} {
		out := renderSettings(t, view.SettingsData{User: user, HasPassword: tt.hasPassword, TOTPEnabled: tt.withTOTP})
		row := sectionHTML(t, out, "password", "</li>")
		start := strings.Index(out, `action="/settings/password"`)
		if !tt.hasPassword {
			if start >= 0 || strings.Contains(row, "Change password") {
				t.Errorf("%s: offers a password change to an account with no password", tt.name)
			}
			continue
		}
		if start < 0 || !strings.Contains(row, "Change password") {
			t.Fatalf("%s: no password change form", tt.name)
		}
		form := out[start:]
		form = form[:strings.Index(form, "</form>")]
		for _, name := range []string{"password", "new_password", "new_password_confirm"} {
			if !strings.Contains(form, `name="`+name+`"`) {
				t.Errorf("%s: form has no %s field", tt.name, name)
			}
		}
		if got := strings.Contains(form, `name="code"`); got != tt.withTOTP {
			t.Errorf("%s: asks for a two-factor code = %v, want %v", tt.name, got, tt.withTOTP)
		}
	}
}

func TestSettings_PasswordErrorShowsBesideTheControl(t *testing.T) {
	out := renderSettings(t, view.SettingsData{User: model.User{ID: 42, Username: "alice"}, HasPassword: true, PasswordError: "password_mismatch"})
	if row := sectionHTML(t, out, "password", "</li>"); !strings.Contains(row, "didn&#39;t match") {
		t.Errorf("the password row doesn't explain the mismatch: %s", row)
	}
}
