package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const (
	hlSpan = `<span class="hl-k">int</span>`
	hlBox  = `class="hl overflow-x-auto`
)

func renderPage(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestBlob_RendersHighlightedLines(t *testing.T) {
	t.Parallel()
	out := renderPage(t, pages.Blob(view.BlobData{
		Path:  "main.c",
		Lines: []service.CodeLine{{Num: 1, Text: "int", HTML: hlSpan}, {Num: 2, Text: "a<b"}},
	}))
	for _, want := range []string{hlBox, `whitespace-pre">` + hlSpan + `</td>`, `whitespace-pre">a&lt;b</td>`} {
		if !strings.Contains(out, want) {
			t.Errorf("blob page missing %q", want)
		}
	}
}

func TestBlame_RendersHighlightedLines(t *testing.T) {
	t.Parallel()
	out := renderPage(t, pages.Blame(view.BlameData{
		Path:  "main.c",
		Lines: []service.BlameLine{{LineNum: 1, Text: "int", HTML: hlSpan}},
	}))
	for _, want := range []string{`class="hl overflow-x-auto"`, `whitespace-pre">` + hlSpan + `</td>`} {
		if !strings.Contains(out, want) {
			t.Errorf("blame page missing %q", want)
		}
	}
}

func TestGistDetail_HighlightsByFilename(t *testing.T) {
	t.Parallel()
	out := renderPage(t, pages.GistDetail(view.GistDetailData{
		Gist: model.Gist{ID: "g1"},
		Files: []model.GistFile{
			{Filename: "main.go", Content: "package main\n"},
			{Filename: "notes.txt", Content: "a<b"},
		},
	}))
	if !strings.Contains(out, `<pre class="hl `) {
		t.Errorf("gist page missing hl container")
	}
	if !strings.Contains(out, `<code><span class="hl-k`) {
		t.Errorf("gist Go file has no token spans")
	}
	if !strings.Contains(out, `<code>a&lt;b</code>`) {
		t.Errorf("gist plain-text file should render escaped text")
	}
}

func TestGistDetail_SharesOneHighlightBudgetAcrossFiles(t *testing.T) {
	t.Parallel()
	// 400 kB each: under the per-source cap, three of them over the page's 1 MiB.
	src := strings.Repeat("// "+strings.Repeat("x", 97)+"\n", 4000)
	out := renderPage(t, pages.GistDetail(view.GistDetailData{
		Gist: model.Gist{ID: "g1"},
		Files: []model.GistFile{
			{Filename: "a.go", Content: src},
			{Filename: "b.go", Content: src},
			{Filename: "c.go", Content: src},
		},
	}))
	if n := strings.Count(out, `<code><span class="hl-`); n != 2 {
		t.Errorf("%d highlighted files, want 2: the third is past the page budget", n)
	}
	if n := strings.Count(out, `<code>// `); n != 1 {
		t.Errorf("%d plain files, want 1", n)
	}
}
