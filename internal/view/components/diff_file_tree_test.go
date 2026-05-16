package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDiffFileTree_BuildsTreeAndRendersStats(t *testing.T) {
	items := []DiffFileTreeItem{
		{Path: "main.go", Anchor: "diff-0", Added: 12, Deleted: 3},
		{Path: "internal/util.go", Anchor: "diff-1", Added: 0, Deleted: 8},
		{Path: "internal/view/pages/pulls.templ", Anchor: "diff-2", Added: 58, Deleted: 4},
	}

	root := buildDiffTree(items)
	if root.findDir("internal") == nil {
		t.Fatal("expected 'internal' directory node")
	}
	if root.findDir("internal").findDir("view") == nil {
		t.Fatal("expected nested 'internal/view' directory node")
	}

	var buf bytes.Buffer
	if err := DiffFileTree(items).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, s := range []string{"main.go", "util.go", "pulls.templ", "internal", "+12", "-3", "-8", "+58", "-4"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q", s)
		}
	}
	if strings.Contains(out, "+0") {
		t.Errorf("unexpected +0 in output (zero additions should be suppressed)")
	}
}
