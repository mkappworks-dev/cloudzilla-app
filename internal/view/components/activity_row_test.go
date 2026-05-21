package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActivityRow_RendersFields(t *testing.T) {
	var buf bytes.Buffer
	err := ActivityRow(ActivityRowData{
		Kind:       "pr_opened",
		Actor:      "alice",
		RepoName:   "acme/foo",
		Subject:    "PR #42: do thing",
		SubjectURL: "/acme/foo/pulls/42",
		When:       "2h ago",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()

	for _, want := range []string{
		"alice",
		"/alice",
		"opened",
		"PR #42: do thing",
		"/acme/foo/pulls/42",
		"acme/foo",
		"2h ago",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestActivityRow_NoSubject(t *testing.T) {
	var buf bytes.Buffer
	err := ActivityRow(ActivityRowData{
		Kind:       "push",
		Actor:      "bob",
		RepoName:   "acme/bar",
		Subject:    "",
		SubjectURL: "",
		When:       "5 minutes ago",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()

	if !strings.Contains(s, "acme/bar") {
		t.Errorf("expected repo name %q in output", "acme/bar")
	}
	if !strings.Contains(s, " in ") {
		t.Errorf("expected \" in \" before repo name when Subject is empty")
	}
	if strings.Contains(s, `href=""`) {
		t.Errorf("unexpected empty href in output when Subject is empty")
	}
}
