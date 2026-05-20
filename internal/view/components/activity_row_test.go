package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActivityRow_RendersVerbAndLinks(t *testing.T) {
	data := ActivityRowData{
		Kind:       "pr_opened",
		Actor:      "alice",
		RepoName:   "alice/demo",
		Subject:    "PR #1",
		SubjectURL: "/alice/demo/pulls/1",
		When:       "2h ago",
	}
	var buf bytes.Buffer
	ActivityRow(data).Render(context.Background(), &buf)
	out := buf.String()
	for _, s := range []string{"alice", "opened", "PR #1", "/alice/demo", "/alice/demo/pulls/1", "2h ago"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in: %s", s, out)
		}
	}
}

func TestActivityVerb_KnownAndUnknown(t *testing.T) {
	cases := map[string]string{
		"pr_opened":    "opened",
		"pr_merged":    "merged",
		"issue_closed": "closed",
		"comment":      "commented on",
		"mystery":      "updated",
	}
	for kind, want := range cases {
		if got := activityVerb(kind); got != want {
			t.Errorf("activityVerb(%q) = %q, want %q", kind, got, want)
		}
	}
}
