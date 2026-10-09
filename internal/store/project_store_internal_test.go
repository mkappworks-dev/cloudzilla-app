package store

import (
	"strings"
	"testing"
)

func TestSplitNote(t *testing.T) {
	long := strings.Repeat("x", 130)
	cases := []struct{ in, title, rest string }{
		{"one line", "one line", ""},
		{"head\nbody\nmore", "head", "body\nmore"},
		{"\n  padded\nrest", "padded", "rest"},
		{long + "\nmore", long[:120], long + "\nmore"},
		{"héllo", "héllo", ""},
	}
	for _, c := range cases {
		title, rest := splitNote(c.in)
		if title != c.title || rest != c.rest {
			t.Errorf("splitNote(%q) = (%q, %q), want (%q, %q)", c.in, title, rest, c.title, c.rest)
		}
	}
}
