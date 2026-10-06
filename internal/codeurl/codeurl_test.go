package codeurl_test

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
)

func TestEscape(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"a/b", "a/b"},
		{`odd "q'uote"/note.txt`, "odd%20%22q%27uote%22/note.txt"},
		{"hash#q?", "hash%23q%3F"},
		{"pct%41", "pct%2541"},
		{"feature/x", "feature/x"},
	}
	for _, tt := range tests {
		if got := codeurl.Escape(tt.in); got != tt.want {
			t.Errorf("Escape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPath(t *testing.T) {
	tests := []struct{ kind, ref, path, want string }{
		{"blob", "feature/a#b", "d/f.txt", "/o/r/blob/feature/a%23b/d/f.txt"},
		{"tree", "main", "", "/o/r/tree/main"},
	}
	for _, tt := range tests {
		if got := codeurl.Path("o", "r", tt.kind, tt.ref, tt.path); got != tt.want {
			t.Errorf("Path(%q, %q, %q) = %q, want %q", tt.kind, tt.ref, tt.path, got, tt.want)
		}
	}
}
