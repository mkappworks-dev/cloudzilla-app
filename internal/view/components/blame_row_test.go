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
		CommittedAt: time.Now(), CommitURL: "/x/y/commit/abc1234",
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
