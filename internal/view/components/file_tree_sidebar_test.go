package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestFileTreeSidebar_RendersHierarchy(t *testing.T) {
	nodes := []TreeNode{
		{Name: "internal", IsDir: true, Href: "/o/r/tree/main/internal"},
		{Name: "main.go", IsDir: false, Href: "/o/r/blob/main/main.go"},
	}
	var buf bytes.Buffer
	if err := FileTreeSidebar(nodes, "main.go").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "internal") || !strings.Contains(out, "main.go") {
		t.Errorf("expected tree entries, got: %s", out)
	}
	if !strings.Contains(out, `aria-current="page"`) {
		t.Errorf("expected aria-current on active file, got: %s", out)
	}
}
