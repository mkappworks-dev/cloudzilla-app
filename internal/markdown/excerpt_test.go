package markdown_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
)

func TestExcerptLines(t *testing.T) {
	cases := []struct {
		name string
		src  string
		max  int
		want []string
	}{
		{"empty", "", 100, nil},
		{"headings and bullets stay apart", "## What's new\n\n- **Faster** clones via `git-upload-pack`\n- [Docs](https://x.test) & fixes", 200, []string{"What's new", "Faster clones via git-upload-pack", "Docs & fixes"}},
		{"code block and html skipped", "Intro\n\n```go\nfmt.Println(1)\n```\n\n<div>raw</div>\n\nOutro", 200, []string{"Intro", "Outro"}},
		{"image alt dropped, autolink kept", "![logo](x.png) See https://example.test/a", 200, []string{"See https://example.test/a"}},
		{"entities decoded", "Tom &amp; Jerry", 200, []string{"Tom & Jerry"}},
		{"soft breaks collapse inside a block", "one\ntwo", 200, []string{"one two"}},
		{"truncated on a rune boundary", "héllo wörld", 7, []string{"héllo w…"}},
		{"budget spans blocks", "abc\n\ndefgh", 5, []string{"abc", "de…"}},
		{"no ellipsis at exact length", "abc", 3, []string{"abc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := markdown.ExcerptLines(tc.src, tc.max); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExcerptLines(%q, %d) = %q, want %q", tc.src, tc.max, got, tc.want)
			}
		})
	}
}

func TestExcerptLines_StopsWalkingLongBodies(t *testing.T) {
	src := strings.Repeat("A paragraph.\n\n", 10000)
	got := markdown.ExcerptLines(src, 20)
	if want := []string{"A paragraph.", "A paragr…"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ExcerptLines = %q, want %q", got, want)
	}
}
