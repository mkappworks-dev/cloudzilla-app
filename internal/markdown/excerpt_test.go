package markdown_test

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
)

func TestExcerpt(t *testing.T) {
	cases := []struct {
		name string
		src  string
		max  int
		want string
	}{
		{"empty", "", 100, ""},
		{"inline syntax dropped", "## What's new\n\n- **Faster** clones via `git-upload-pack`\n- [Docs](https://x.test) & fixes", 200, "What's new Faster clones via git-upload-pack Docs & fixes"},
		{"code block and html skipped", "Intro\n\n```go\nfmt.Println(1)\n```\n\n<div>raw</div>\n\nOutro", 200, "Intro Outro"},
		{"image alt dropped, autolink kept", "![logo](x.png) See https://example.test/a", 200, "See https://example.test/a"},
		{"entities decoded", "Tom &amp; Jerry", 200, "Tom & Jerry"},
		{"truncated on a rune boundary", "héllo wörld", 7, "héllo w…"},
		{"no ellipsis at exact length", "abc", 3, "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := markdown.Excerpt(tc.src, tc.max); got != tc.want {
				t.Errorf("Excerpt(%q, %d) = %q, want %q", tc.src, tc.max, got, tc.want)
			}
		})
	}
}

func TestExcerpt_StopsWalkingLongBodies(t *testing.T) {
	src := strings.Repeat("A paragraph.\n\n", 10000)
	if got, want := markdown.Excerpt(src, 20), "A paragraph. A parag…"; got != want {
		t.Errorf("Excerpt = %q, want %q", got, want)
	}
}
