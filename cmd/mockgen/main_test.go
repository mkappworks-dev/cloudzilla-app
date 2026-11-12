package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate_IsDeterministicAndStatic(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"index.html", "COVERAGE.md"} {
		seed := "<!-- " + regionStart + " -->\n<!-- " + regionEnd + " -->\n"
		if err := os.WriteFile(filepath.Join(dir, f), []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string][]byte {
		out := map[string][]byte{}
		files, err := filepath.Glob(filepath.Join(dir, "generated", "*.html"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			out[filepath.Base(f)] = b
		}
		return out
	}

	if err := generate(context.Background(), dir, registry()); err != nil {
		t.Fatal(err)
	}
	first := read()
	if len(first) == 0 {
		t.Fatal("no pages generated")
	}
	if err := generate(context.Background(), dir, registry()); err != nil {
		t.Fatal(err)
	}
	for name, b := range read() {
		if !bytes.Equal(b, first[name]) {
			t.Errorf("%s differs between two runs", name)
		}
		s := string(b)
		for _, banned := range []string{"/static/htmx", "/static/alpine", "/static/main.css"} {
			if strings.Contains(s, banned) {
				t.Errorf("%s still references %q", name, banned)
			}
		}
		if n := strings.Count(s, "<script"); n != 2 {
			t.Errorf("%s has %d scripts, want the theme bootstrap and mock-ui.js", name, n)
		}
	}
}
