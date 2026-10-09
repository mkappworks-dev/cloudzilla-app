package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderHighlight(t *testing.T, text, query string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Highlight(text, query).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func marked(out string) []string {
	var got []string
	for _, part := range strings.Split(out, "<mark")[1:] {
		inner := part[strings.Index(part, ">")+1 : strings.Index(part, "</mark>")]
		got = append(got, inner)
	}
	return got
}

func TestHighlight_MarksWordPrefixes(t *testing.T) {
	cases := []struct {
		name, text, query string
		want              []string
	}{
		{"prefix of a word in a path", "acme/brave-core", "bra", []string{"brave"}},
		{"case-insensitive", "BRAVE shields crash", "brave", []string{"BRAVE"}},
		{"every query word", "Brave browser crash", "brave cra", []string{"Brave", "crash"}},
		{"unicode letters", "Ünïcode ünïcode", "ünï", []string{"Ünïcode", "ünïcode"}},
		{"a repeated word is marked each time", "go go go", "go", []string{"go", "go", "go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := marked(renderHighlight(t, tc.text, tc.query))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("marked %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHighlight_LeavesOtherTextAlone(t *testing.T) {
	for name, tc := range map[string]struct{ text, query string }{
		"mid-word is not a prefix": {"brave", "rave"},
		"empty query":              {"brave", ""},
		"only punctuation":         {"brave", "-- !!"},
		"no match":                 {"brave", "zzz"},
	} {
		t.Run(name, func(t *testing.T) {
			if out := renderHighlight(t, tc.text, tc.query); strings.Contains(out, "<mark") || out != tc.text {
				t.Errorf("got %q, want the text unchanged", out)
			}
		})
	}
}

func TestHighlight_EscapesHTML(t *testing.T) {
	for _, query := range []string{"script", "zzz", ""} {
		out := renderHighlight(t, `<script>alert(1)</script>`, query)
		if strings.Contains(out, "<script>") {
			t.Errorf("query %q: unescaped markup in %s", query, out)
		}
	}
	if out := renderHighlight(t, `<script>alert(1)</script>`, "script"); !strings.Contains(out, "&lt;<mark") {
		t.Errorf("the match should sit inside the escaped text: %s", out)
	}
}
