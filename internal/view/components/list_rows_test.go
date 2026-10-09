package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestIssueListRow(t *testing.T) {
	var buf bytes.Buffer
	d := IssueListRowData{
		Href: "/alice/app/issues/4", Repo: "alice/app", Number: 4, Title: "Crash <on> load",
		Author: "bob", State: "open", Private: true, OpenedAt: "2 days ago", OpenedISO: "2026-10-07T00:00:00Z",
		Labels: []LabelChip{{Name: "bug", Color: "#ff0000"}},
	}
	if err := IssueListRow(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`href="/alice/app/issues/4"`, "<span>alice/app</span>", "#4", "opened by bob", "2 days ago", "Private", "bug", "Crash &lt;on&gt; load"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}

	buf.Reset()
	d.Repo, d.Private, d.OpenedAt, d.Labels = "", false, "", nil
	if err := IssueListRow(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, unwanted := range []string{"<span>alice/app</span>", `aria-label="Private"`, "<time", `aria-label="Labels"`} {
		if strings.Contains(buf.String(), unwanted) {
			t.Errorf("unexpected %q in %s", unwanted, buf.String())
		}
	}
}

func TestRepoListRow(t *testing.T) {
	var buf bytes.Buffer
	d := RepoListRowData{
		Owner: "alice", Name: "app", Description: "does things", Private: true,
		CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), ShowStats: true, Stars: 3, Forks: 1,
	}
	if err := RepoListRow(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{`href="/alice/app"`, "does things", "Private", "Jan 2, 2026", `title="Stars"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in %s", want, buf.String())
		}
	}

	buf.Reset()
	if err := RepoListRow(RepoListRowData{Owner: "alice", Name: "app"}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, unwanted := range []string{"Private", "Created", `title="Stars"`} {
		if strings.Contains(buf.String(), unwanted) {
			t.Errorf("unexpected %q in %s", unwanted, buf.String())
		}
	}
}

func TestPersonListRow(t *testing.T) {
	var buf bytes.Buffer
	if err := PersonListRow("brave-software", "Brave Software", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{`href="/brave-software"`, "Brave Software"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in %s", want, buf.String())
		}
	}
}
