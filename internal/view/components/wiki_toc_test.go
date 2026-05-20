package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func TestWikiTOC(t *testing.T) {
	t.Run("renders entries with anchors and text", func(t *testing.T) {
		entries := []service.WikiTOCEntry{
			{Level: 1, Anchor: "intro", Text: "Introduction"},
			{Level: 2, Anchor: "layered-design", Text: "Layered design"},
			{Level: 3, Anchor: "stores", Text: "Stores"},
			{Level: 4, Anchor: "deep", Text: "Deep section"},
		}
		var buf bytes.Buffer
		if err := WikiTOC(entries).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render error: %v", err)
		}
		out := buf.String()

		for _, want := range []string{
			`aria-label="On this page"`,
			"On this page",
			`#intro`, "Introduction",
			`#layered-design`, "Layered design",
			`#stores`, "Stores",
			`#deep`, "Deep section",
			"12px", "24px",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in output:\n%s", want, out)
			}
		}
	})

	t.Run("empty slice renders without panicking", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WikiTOC([]service.WikiTOCEntry{}).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "On this page") {
			t.Errorf("expected heading even for empty entries, got:\n%s", out)
		}
	})

	t.Run("nil slice renders without panicking", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WikiTOC(nil).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render error: %v", err)
		}
	})
}
