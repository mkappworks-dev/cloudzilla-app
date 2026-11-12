package components

import (
	"testing"

	"github.com/a-h/templ"
)

func TestInvalid(t *testing.T) {
	base := templ.Attributes{"id": "email", "aria-describedby": "email-hint"}

	if got := Invalid(base, "email", ""); got["aria-invalid"] != nil || got["aria-describedby"] != "email-hint" {
		t.Errorf("no message must leave attrs alone, got %v", got)
	}

	got := Invalid(base, "email", "Enter a valid email address.")
	if got["aria-invalid"] != "true" {
		t.Errorf("aria-invalid = %v", got["aria-invalid"])
	}
	if got["aria-describedby"] != "email-hint email-error" {
		t.Errorf("aria-describedby = %v, want hint then error", got["aria-describedby"])
	}
	if base["aria-invalid"] != nil {
		t.Error("Invalid mutated its input")
	}
}
