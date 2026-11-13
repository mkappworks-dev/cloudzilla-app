package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunStatusIcon_EachStatusHasItsLabelColourAndGlyph(t *testing.T) {
	cases := []struct{ in, label, class, glyph string }{
		{"queued", "Queued", "text-muted-foreground", "M12 7v5l3 2"},
		{"waiting", "Approval required", "text-warning", "M10 9v6M14 9v6"},
		{"running", "Running", "motion-safe:animate-pulse", `r="3.5"`},
		{"success", "Succeeded", "text-success", "M9 12l2 2 4-4"},
		{"failure", "Failed", "text-destructive", "M15 9l-6 6M9 9l6 6"},
		{"cancelled", "Cancelled", "text-muted-foreground", "M4.93 4.93l14.14 14.14"},
		{"skipped", "Skipped", "text-muted-foreground", "M8 12h8"},
		// Commit-status states fold onto the same set.
		{"pending", "Running", "motion-safe:animate-pulse", `r="3.5"`},
		{"error", "Failed", "text-destructive", "M15 9l-6 6M9 9l6 6"},
		{"", "Skipped", "text-muted-foreground", "M8 12h8"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := RunStatusIcon(c.in).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render %q: %v", c.in, err)
		}
		out := buf.String()
		for _, want := range []string{`aria-label="` + c.label + `"`, c.class, c.glyph, `width="14"`} {
			if !strings.Contains(out, want) {
				t.Errorf("%q: missing %q in %s", c.in, want, out)
			}
		}
	}
}

func TestRunStatusIconSized_UsesTheSize(t *testing.T) {
	var buf bytes.Buffer
	if err := RunStatusIconSized("success", 18).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `width="18" height="18"`) {
		t.Errorf("got %s", buf.String())
	}
}
