package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestBlameRow_RendersAllFields(t *testing.T) {
	line := BlameLine{
		LineNumber: 42, Code: "package foo",
		AuthorName: "alice", ShortSHA: "abc1234",
		CommittedAt: time.Now(), CommitURL: "/x/y/commit/abc1234", HunkStart: true,
	}
	var buf bytes.Buffer
	if err := BlameRow(line).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, s := range []string{"alice", "abc1234", "42", "package foo"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in: %s", s, out)
		}
	}
}

func renderBlameRow(t *testing.T, line BlameLine) string {
	t.Helper()
	var buf bytes.Buffer
	if err := BlameRow(line).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestBlameRow_LinksResolvedUsernameAndKeepsEmailInTooltip(t *testing.T) {
	out := renderBlameRow(t, BlameLine{
		LineNumber: 1, AuthorName: "ada", AuthorUsername: "ada-silva", AuthorEmail: "2+ada-silva@users.noreply.localhost",
		ShortSHA: "abc1234", CommittedAt: time.Now(), CommitURL: "/x/y/commit/abc", HunkStart: true,
	})
	for _, s := range []string{`href="/ada-silva"`, `title="2+ada-silva@users.noreply.localhost"`, ">ada-silva</a>"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in: %s", s, out)
		}
	}
}

func TestBlameRow_UnresolvedAuthorShowsNameWithoutLink(t *testing.T) {
	out := renderBlameRow(t, BlameLine{
		LineNumber: 1, AuthorName: "Stranger", AuthorEmail: "s@example.com",
		ShortSHA: "abc1234", CommittedAt: time.Now(), CommitURL: "/x/y/commit/abc", HunkStart: true,
	})
	if !strings.Contains(out, ">Stranger</span>") || strings.Contains(out, `href="/Stranger"`) {
		t.Errorf("want an unlinked name in: %s", out)
	}
}

func TestBlameRow_ContinuationDrawsAMarkerInsteadOfMetadata(t *testing.T) {
	out := renderBlameRow(t, BlameLine{LineNumber: 2, AuthorName: "ada", ShortSHA: "abc1234", CommittedAt: time.Now()})
	if !strings.Contains(out, "Same commit as the line above") || strings.Contains(out, "abc1234") {
		t.Errorf("continuation row = %s", out)
	}
}
