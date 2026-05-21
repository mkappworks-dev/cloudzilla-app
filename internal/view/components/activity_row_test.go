package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActivityRow_PROpened(t *testing.T) {
	var buf bytes.Buffer
	err := ActivityRow(ActivityRowData{
		Kind:     "pr_opened",
		Actor:    "alice",
		RepoName: "acme/foo",
		Ref:      "#42",
		RefURL:   "/acme/foo/pulls/42",
		Title:    "feat: do thing",
		When:     "2h ago",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()

	for _, want := range []string{
		"alice",
		"/alice",
		"opened",
		"#42",
		"/acme/foo/pulls/42",
		"acme/foo",
		"feat: do thing",
		"2h ago",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestActivityRow_Star(t *testing.T) {
	var buf bytes.Buffer
	err := ActivityRow(ActivityRowData{
		Kind:     "star",
		Actor:    "priya",
		RepoName: "acme/bar",
		When:     "2d ago",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()

	if !strings.Contains(s, "priya") {
		t.Errorf("missing actor in output")
	}
	if !strings.Contains(s, "starred") {
		t.Errorf("missing verb in output")
	}
	if !strings.Contains(s, "acme/bar") {
		t.Errorf("missing repo name in output")
	}
	if !strings.Contains(s, " in ") {
		t.Errorf("expected \" in \" before repo name")
	}
	if strings.Contains(s, `href=""`) {
		t.Errorf("unexpected empty href in output")
	}
	if strings.Contains(s, "#") {
		t.Errorf("unexpected # ref in star row")
	}
}
