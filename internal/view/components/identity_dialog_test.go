package components_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func renderIdentityDialog(t *testing.T, f components.ConfirmFactors) string {
	t.Helper()
	var sb strings.Builder
	if err := components.ConfirmIdentityDialog(f).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestConfirmIdentityDialog_AsksForTheFactorsInsideTheDialog(t *testing.T) {
	out := renderIdentityDialog(t, components.ConfirmFactors{Password: true, Code: true})

	dialog, _, ok := strings.Cut(out, "</dialog>")
	if !ok || !strings.Contains(dialog, `<dialog id="cz-identity-dialog"`) {
		t.Fatalf("no #cz-identity-dialog:\n%s", out)
	}
	for _, want := range []string{
		`name="password"`,
		`name="code"`,
		"This change needs your password and a two-factor code.",
	} {
		if !strings.Contains(dialog, want) {
			t.Errorf("dialog lacks %s", want)
		}
	}
	pw := regexp.MustCompile(`<input[^>]*name="password"[^>]*>`).FindString(dialog)
	if !strings.Contains(pw, "required") {
		t.Errorf("password is optional: %s", pw)
	}
	if !regexp.MustCompile(`[\s"]w-full[\s"]`).MatchString(pw) {
		t.Errorf("password field does not span the dialog: %s", pw)
	}
	reveal := regexp.MustCompile(`<button[^>]*data-password-reveal[^>]*>`).FindString(dialog)
	for _, want := range []string{`type="button"`, `aria-label="Show password"`, `aria-pressed="false"`} {
		if !strings.Contains(reveal, want) {
			t.Errorf("show-password toggle lacks %s: %q", want, reveal)
		}
	}
}

// Inline confirm fields keep their compact field and gain no toggle.
func TestConfirmFields_InlineHasNoRevealToggle(t *testing.T) {
	var sb strings.Builder
	if err := components.ConfirmFields("x", components.ConfirmFactors{Password: true}, true).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	if out := sb.String(); strings.Contains(out, "data-password-reveal") || !strings.Contains(out, "w-56") {
		t.Errorf("inline password field changed:\n%s", out)
	}
}

func TestConfirmIdentityDialog_DisablesConfirmWhenUnavailable(t *testing.T) {
	ok := regexp.MustCompile(`<button[^>]*data-identity-ok[^>]*>`)
	disabled := regexp.MustCompile(`\sdisabled[\s>=]`)
	if btn := ok.FindString(renderIdentityDialog(t, components.ConfirmFactors{Unavailable: true})); !disabled.MatchString(btn) {
		t.Errorf("confirm button is enabled with no way to confirm: %s", btn)
	}
	if btn := ok.FindString(renderIdentityDialog(t, components.ConfirmFactors{Password: true})); btn == "" || disabled.MatchString(btn) {
		t.Errorf("confirm button missing or disabled for a password account: %q", btn)
	}
}
