package service

import (
	"strings"
	"testing"
)

func TestExtractWikiTOC(t *testing.T) {
	html := `
<h1 id="intro">Intro</h1>
<p>...</p>
<h2 id="setup">Setup</h2>
<p>...</p>
<h2 id="usage">Usage</h2>
<h3 id="basic">Basic</h3>
<h3 id="advanced">Advanced</h3>
`
	toc := ExtractWikiTOC(html)
	if len(toc) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(toc))
	}
	if toc[0].Level != 1 || toc[0].Anchor != "intro" {
		t.Errorf("first entry mismatch: %+v", toc[0])
	}
	if toc[3].Level != 3 || !strings.EqualFold(toc[3].Text, "Basic") {
		t.Errorf("nested entry mismatch: %+v", toc[3])
	}
}

func TestExtractWikiTOC_NoExplicitID(t *testing.T) {
	html := `<h2>Hello World</h2>`
	toc := ExtractWikiTOC(html)
	if len(toc) != 1 || toc[0].Anchor != "hello-world" {
		t.Fatalf("expected slugified anchor, got %+v", toc)
	}
}
