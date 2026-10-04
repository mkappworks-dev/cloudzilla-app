package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderSidebar(t *testing.T, nodes []TreeNode) string {
	t.Helper()
	var buf bytes.Buffer
	if err := FileTreeSidebar("/o/r", nodes).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestFileTreeSidebar_RendersHierarchy(t *testing.T) {
	out := renderSidebar(t, []TreeNode{
		{Name: "internal", IsDir: true, Href: "/o/r/tree/main/internal", Path: "internal", ChildrenURL: "/fragments/o/r/tree/main/internal"},
		{Name: "main.go", IsDir: false, Href: "/o/r/blob/main/main.go", Path: "main.go", IsActive: true},
	})
	for _, want := range []string{
		"internal", "main.go",
		`aria-current="page"`,
		`data-path="internal"`,
		`x-on:cz-tree-collapse-all.window="open = false"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestFileTreeSidebar_Header(t *testing.T) {
	out := renderSidebar(t, nil)
	for _, want := range []string{
		`/static/file_tree.js`,
		`x-data="fileTree"`,
		`data-repo-path="/o/r"`,
		`aria-label="Collapse all folders"`,
		`x-on:click="collapseAll()"`,
		`x-on:keydown.escape="filter = ''"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	for _, gone := range []string{`aria-label="Clear filter"`, `M3 6h18M3 12h18M3 18h18`} {
		if strings.Contains(out, gone) {
			t.Errorf("want no %q in:\n%s", gone, out)
		}
	}
}

func TestFileTreeSidebar_PathsStayOutOfScript(t *testing.T) {
	evil := `x" x-init="alert(1)`
	out := renderSidebar(t, []TreeNode{{Name: evil, IsDir: true, Path: evil, Href: "/o/r/tree/main/x"}})
	if strings.Contains(out, `x-init="alert(1)"`) {
		t.Errorf("a folder name escaped its attribute:\n%s", out)
	}
}

func TestFileTreeSidebar_ChevronRendersInItsState(t *testing.T) {
	out := renderSidebar(t, []TreeNode{
		{Name: "open", IsDir: true, IsOpen: true, Path: "open", Href: "/o/r/tree/main/open"},
		{Name: "shut", IsDir: true, Path: "shut", Href: "/o/r/tree/main/shut"},
	})
	if !strings.Contains(out, `class="transition-transform rotate-90"`) {
		t.Errorf("open folder's chevron must render rotated, or it animates on every page load:\n%s", out)
	}
	if got := strings.Count(out, `rotate-90"`); got != 1 {
		t.Errorf("want only the open folder's chevron rendered rotated, got %d:\n%s", got, out)
	}
	if !strings.Contains(out, `:class="{ 'rotate-90': open }"`) {
		t.Errorf("chevron binding must use the object form, which also removes a server-rendered class:\n%s", out)
	}
}
