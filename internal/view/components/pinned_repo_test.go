package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPinnedRepo_RendersAllFields(t *testing.T) {
	d := PinnedRepoData{
		OwnerName: "alice", Name: "demo",
		Description:   "A demo project",
		Language:      "Go",
		LanguageColor: "#00ADD8",
		Stars:         42,
	}
	var buf bytes.Buffer
	PinnedRepo(d).Render(context.Background(), &buf)
	out := buf.String()
	for _, s := range []string{"alice/demo", "A demo project", "Go", "#00ADD8", "★ 42", "/alice/demo"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in: %s", s, out)
		}
	}
}

func TestPinnedRepo_OmitsEmptyOptionals(t *testing.T) {
	d := PinnedRepoData{OwnerName: "alice", Name: "demo", Stars: 0}
	var buf bytes.Buffer
	PinnedRepo(d).Render(context.Background(), &buf)
	out := buf.String()
	if strings.Contains(out, "background-color: ") {
		t.Errorf("expected language dot omitted when Language empty: %s", out)
	}
	if strings.Contains(out, "line-clamp-2") {
		t.Errorf("expected description block omitted when Description empty: %s", out)
	}
}
