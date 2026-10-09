package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func assertHas(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %s", w, out)
		}
	}
}

func assertLacks(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(out, u) {
			t.Errorf("unexpected %q in %s", u, out)
		}
	}
}

func TestIssueListRow(t *testing.T) {
	d := IssueListRowData{
		Href: "/alice/app/issues/4", Repo: "alice/app", Number: 4, Title: "Crash <on> load",
		Author: "bob", State: "open", Private: true, Time: "2d ago",
		Labels: []LabelChip{{Name: "bug", Color: "#ff0000"}}, Priority: "P0", Comments: 3,
		LiAttrs: templ.Attributes{"data-name": "alice/app Crash"},
	}
	assertHas(t, render(t, IssueListRow(d)),
		`href="/alice/app/issues/4"`, "alice/app", "#4", ">bob<", "2d ago", `aria-label="Private"`,
		"bug", "P0", `title="3 comments"`, "Crash &lt;on&gt; load", `data-name="alice/app Crash"`, "Open")

	d.Repo, d.Private, d.Time, d.Labels, d.Priority, d.Comments, d.State = "", false, "", nil, "", 0, "closed"
	out := render(t, IssueListRow(d))
	assertHas(t, out, "Closed")
	assertLacks(t, out, "<time", `aria-label="Private"`, "P0", "comments")
}

func TestRepoListRow(t *testing.T) {
	d := RepoListRowData{
		Owner: "alice", Name: "app", Description: "does things", Private: true, Template: true,
		Role: "Owner", RoleClass: "text-x", Language: "Go", LangClass: "bg-go", Time: "Updated 2h ago",
		Topics: []string{"cli"}, ShowStats: true, Stars: 3, Forks: 1, Commits: 12,
	}
	assertHas(t, render(t, RepoListRow(d)),
		`href="/alice/app"`, "does things", "Private", "Template", "Owner", "Go", "Updated 2h ago", "cli",
		`title="3 stars"`, `title="1 fork"`, `title="12 commits"`)

	out := render(t, RepoListRow(RepoListRowData{Owner: "alice", Name: "app"}))
	assertHas(t, out, "Public")
	assertLacks(t, out, "Private", "Template", "Archived", `title="0 stars"`, "commit")
}

func TestPersonListRow(t *testing.T) {
	assertHas(t, render(t, PersonListRow(PersonListRowData{Name: "brave-software", Subtitle: "Brave Software", Badge: "Owner"})),
		`href="/brave-software"`, "Brave Software", "Owner")
	assertLacks(t, render(t, PersonListRow(PersonListRowData{Name: "brave-software"})), "<p ")
}
