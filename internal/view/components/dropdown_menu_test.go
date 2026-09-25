package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderMenuItem(t *testing.T, attrs templ.Attributes) string {
	t.Helper()
	var buf bytes.Buffer
	if err := DropdownMenuItem(attrs).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// Browsers keep the first of duplicate attributes, so a caller's type must
// replace the default rather than follow it.
func TestDropdownMenuItem_TypeOverride(t *testing.T) {
	out := renderMenuItem(t, templ.Attributes{"type": "submit"})
	if n := strings.Count(out, "type="); n != 1 {
		t.Fatalf("want exactly one type attribute, got %d in %s", n, out)
	}
	if !strings.Contains(out, `type="submit"`) {
		t.Errorf("want type=\"submit\", got %s", out)
	}
}

func TestDropdownMenuItem_DefaultTypeButton(t *testing.T) {
	out := renderMenuItem(t, nil)
	if !strings.Contains(out, `type="button"`) {
		t.Errorf("want default type=\"button\", got %s", out)
	}
}
