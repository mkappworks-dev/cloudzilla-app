package components

import (
	"bytes"
	"context"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
)

func renderToString(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestCodeText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		html template.HTML
		want string
	}{
		{"highlighted markup is emitted unescaped", "a<b", `<span class="hl-k">a&lt;b</span>`, `<span class="hl-k">a&lt;b</span>`},
		{"plain text is escaped", "a<b", "", "a&lt;b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := renderToString(t, CodeText(tc.text, tc.html)); got != tc.want {
				t.Errorf("CodeText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiffCode(t *testing.T) {
	t.Parallel()
	hl := template.HTML(`<span class="hl-k">int</span>`)

	t.Run("highlighted add line colors only the marker", func(t *testing.T) {
		t.Parallel()
		got := renderToString(t, DiffCode("add", "int", hl))
		if !strings.HasPrefix(got, `<span class="text-success">+</span>`) || !strings.HasSuffix(got, string(hl)) {
			t.Errorf("DiffCode = %q", got)
		}
	})
	t.Run("highlighted delete line", func(t *testing.T) {
		t.Parallel()
		got := renderToString(t, DiffCode("del", "int", hl))
		if !strings.HasPrefix(got, `<span class="text-destructive">-</span>`) {
			t.Errorf("DiffCode = %q", got)
		}
	})
	t.Run("plain lines carry the marker as text", func(t *testing.T) {
		t.Parallel()
		for lineType, want := range map[string]string{"add": "+x&lt;y", "del": "-x&lt;y", "ctx": " x&lt;y"} {
			if got := renderToString(t, DiffCode(lineType, "x<y", "")); got != want {
				t.Errorf("DiffCode(%q) = %q, want %q", lineType, got, want)
			}
		}
	})
}

func TestBlameRow_CodeCellHasNoStrayWhitespace(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]BlameLine{
		"plain":       {Code: "x < 1", CommittedAt: time.Now()},
		"highlighted": {Code: "x < 1", CodeHTML: `<span class="hl-n">x</span>`, CommittedAt: time.Now()},
	} {
		out := renderToString(t, BlameRow(line))
		cell := out[strings.LastIndex(out, `whitespace-pre">`)+len(`whitespace-pre">`):]
		cell = strings.TrimSuffix(cell, "</td></tr>")
		want := "x &lt; 1"
		if line.CodeHTML != "" {
			want = string(line.CodeHTML)
		}
		if cell != want {
			t.Errorf("%s: code cell = %q, want %q", name, cell, want)
		}
	}
}
