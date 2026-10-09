package pages

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func testBasePage() view.BasePage { return view.BasePage{} }

func TestDeviceConfirm_Content(t *testing.T) {
	html := renderPage(t, DeviceConfirm(view.DeviceConfirmData{
		BasePage: testBasePage(), Username: "mk", UserCode: "BCDF-GHJK",
		DeviceName: `<script>alert(1)</script>`, RequesterIP: "203.0.113.7", RequestedAt: "just now",
		Scopes:   []string{"repo:write", "repo:read"},
		Selected: []string{"repo:write", "repo:read"},
		Confirm:  components.ConfirmFactors{Password: true, Code: true},
	}))
	for _, want := range []string{
		"Authorize cz", "mk", "BCDF-GHJK", "unverified", "203.0.113.7", "just now",
		"Only continue if you just ran", "Untick to give less",
		`name="scope" value="repo:write"`, `name="scope" value="repo:read"`,
		`action="/login/device/approve"`, `name="password"`, `name="code"`,
		`name="action"`, `type="submit" value="approve"`, `type="submit" value="deny"`,
		"Settings → Access tokens",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("confirm page is missing %q", want)
		}
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("the device name must be escaped")
	}
	for _, sc := range []string{"repo:write", "repo:read"} {
		if !strings.Contains(html, `value="`+sc+`" checked`) {
			t.Errorf("scope %s should be ticked", sc)
		}
	}
	if strings.Contains(html, `name="user_code"`) {
		t.Error("the code is held in a cookie, never in a form field")
	}
}

func TestDeviceConfirm_OnlySelectedScopesAreTicked(t *testing.T) {
	html := renderPage(t, DeviceConfirm(view.DeviceConfirmData{
		BasePage: testBasePage(), UserCode: "BCDF-GHJK",
		Scopes: []string{"repo:write", "repo:read"}, Selected: []string{"repo:read"},
	}))
	if !strings.Contains(html, `value="repo:read" checked`) || strings.Contains(html, `value="repo:write" checked`) {
		t.Error("only repo:read should be ticked")
	}
	if !strings.Contains(html, `value="repo:write"`) {
		t.Error("repo:write should still be offered")
	}
}

func TestDeviceConfirm_NoFactorsAccount(t *testing.T) {
	html := renderPage(t, DeviceConfirm(view.DeviceConfirmData{BasePage: testBasePage(), UserCode: "BCDF-GHJK", Scopes: []string{"repo:read"}, Confirm: components.ConfirmFactors{Unavailable: true}}))
	if !strings.Contains(html, "no way to confirm") {
		t.Error("an account that cannot confirm should be told so")
	}
}

func TestDeviceEntryAndDone(t *testing.T) {
	entry := renderPage(t, DeviceEntry(view.DeviceEntryData{BasePage: testBasePage(), Error: "That code isn't valid."}))
	for _, want := range []string{`action="/login/device"`, `name="user_code"`, "That code isn&#39;t valid."} {
		if !strings.Contains(entry, want) && !strings.Contains(entry, strings.ReplaceAll(want, "&#39;", "'")) {
			t.Errorf("entry page is missing %q", want)
		}
	}
	if !strings.Contains(renderPage(t, DeviceDone(view.DeviceDoneData{BasePage: testBasePage(), Approved: true})), "return to your terminal") {
		t.Error("approved page should send the user back to the terminal")
	}
	if !strings.Contains(renderPage(t, DeviceDone(view.DeviceDoneData{BasePage: testBasePage()})), "denied") {
		t.Error("denied page should say so")
	}
}

var (
	buttonTagRe    = regexp.MustCompile(`<button[^>]*>`)
	disabledAttrRe = regexp.MustCompile(`\sdisabled[\s=>]`)
)

func TestDeviceConfirm_AuthorizeWaitsForAProviderAccountToConfirm(t *testing.T) {
	disabled := func(f components.ConfirmFactors, action string) bool {
		html := renderPage(t, DeviceConfirm(view.DeviceConfirmData{BasePage: testBasePage(), UserCode: "BCDF-GHJK", Scopes: []string{"repo:read"}, Confirm: f}))
		for _, tag := range buttonTagRe.FindAllString(html, -1) {
			if strings.Contains(tag, `value="`+action+`"`) {
				return disabledAttrRe.MatchString(tag)
			}
		}
		t.Fatalf("no %s button", action)
		return false
	}

	for _, tc := range []struct {
		name string
		f    components.ConfirmFactors
		want bool
	}{
		{"google, not yet signed in", components.ConfirmFactors{Provider: "google"}, true},
		{"google or emailed code", components.ConfirmFactors{Provider: "google", Email: true}, true},
		{"google, signed in again", components.ConfirmFactors{Provider: "google", ProviderReady: true}, false},
		{"password", components.ConfirmFactors{Password: true}, false},
		{"directory password", components.ConfirmFactors{Directory: true}, false},
		{"emailed code only", components.ConfirmFactors{Email: true}, false},
	} {
		if got := disabled(tc.f, "approve"); got != tc.want {
			t.Errorf("%s: Authorize disabled = %v; want %v", tc.name, got, tc.want)
		}
		if disabled(tc.f, "deny") {
			t.Errorf("%s: Deny must stay available", tc.name)
		}
	}

	html := renderPage(t, DeviceConfirm(view.DeviceConfirmData{BasePage: testBasePage(), UserCode: "BCDF-GHJK", Scopes: []string{"repo:read"}, Confirm: components.ConfirmFactors{Provider: "google", Email: true}}))
	if !strings.Contains(html, "gated: true, proven: false") {
		t.Error("typing an emailed code should be able to enable Authorize")
	}
}

func TestDeviceConfirm_ShowsTheViewersIP(t *testing.T) {
	html := renderPage(t, DeviceConfirm(view.DeviceConfirmData{BasePage: testBasePage(), UserCode: "BCDF-GHJK", RequesterIP: "203.0.113.7", ViewerIP: "198.51.100.9", Scopes: []string{"repo:read"}}))
	if !strings.Contains(html, "203.0.113.7") || !strings.Contains(html, "198.51.100.9") || !strings.Contains(html, "This browser") {
		t.Error("the page should show the requester's IP and this browser's")
	}
}
