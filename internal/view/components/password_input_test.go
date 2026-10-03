package components_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func TestPasswordInput(t *testing.T) {
	var sb strings.Builder
	attrs := templ.Attributes{"id": "pw", "name": "password", "required": "required"}
	if err := components.PasswordInput(attrs).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	input, _, ok := strings.Cut(out, "<button")
	if !ok {
		t.Fatalf("no toggle button:\n%s", out)
	}
	for _, want := range []string{`type="password"`, `id="pw"`, `name="password"`, `required`} {
		if !strings.Contains(input, want) {
			t.Errorf("input lacks %s:\n%s", want, input)
		}
	}
	if strings.Count(input, " type=") != 1 {
		t.Errorf("input should carry exactly one static type attribute:\n%s", input)
	}

	button := out[len(input):]
	for _, want := range []string{`type="button"`, `aria-controls="pw"`, `aria-pressed`} {
		if !strings.Contains(button, want) {
			t.Errorf("toggle lacks %s:\n%s", want, button)
		}
	}
}
