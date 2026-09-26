package components

import (
	"bytes"
	"context"
	"slices"
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
	if n := strings.Count(out, "type="); n != 1 {
		t.Fatalf("want exactly one type attribute, got %d in %s", n, out)
	}
	if !strings.Contains(out, `type="button"`) {
		t.Errorf("want default type=\"button\", got %s", out)
	}
}

func TestDropdownMenuItem_TypeAndClassOverride(t *testing.T) {
	out := renderMenuItem(t, templ.Attributes{"type": "submit", "class": "text-destructive"})
	if n := strings.Count(out, "type="); n != 1 {
		t.Fatalf("want exactly one type attribute, got %d in %s", n, out)
	}
	if !strings.Contains(out, `type="submit"`) {
		t.Errorf("want type=\"submit\", got %s", out)
	}
	classes := classAttr.FindAllStringSubmatch(out, -1)
	if len(classes) != 1 {
		t.Fatalf("want exactly one class attribute, got %d in %s", len(classes), out)
	}
	if !slices.Contains(strings.Fields(classes[0][1]), "text-destructive") {
		t.Errorf("caller class missing from %q", classes[0][1])
	}
}

func renderMenuTrigger(t *testing.T, attrs templ.Attributes) string {
	t.Helper()
	var buf bytes.Buffer
	if err := DropdownMenuTrigger(attrs).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestDropdownMenuTrigger_TypeOverride(t *testing.T) {
	out := renderMenuTrigger(t, templ.Attributes{"type": "submit"})
	if n := strings.Count(out, "type="); n != 1 {
		t.Fatalf("want exactly one type attribute, got %d in %s", n, out)
	}
	if !strings.Contains(out, `type="submit"`) {
		t.Errorf("want type=\"submit\", got %s", out)
	}
}

func TestDropdownMenuTrigger_DefaultTypeButton(t *testing.T) {
	out := renderMenuTrigger(t, nil)
	if n := strings.Count(out, "type="); n != 1 {
		t.Fatalf("want exactly one type attribute, got %d in %s", n, out)
	}
	if !strings.Contains(out, `type="button"`) {
		t.Errorf("want default type=\"button\", got %s", out)
	}
}
