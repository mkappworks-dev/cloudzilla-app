package pages_test

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// rowFor returns the rendered <li> whose data-name is name.
func rowFor(t *testing.T, html, name string) string {
	t.Helper()
	i := strings.Index(html, `data-name="`+name+`"`)
	if i < 0 {
		t.Fatalf("no row %q", name)
	}
	row := html[i:]
	return row[:strings.Index(row, "</li>")]
}

func TestAccountPulls_MarksDrafts(t *testing.T) {
	html := renderPage(t, pages.AccountPulls(view.AccountPullsData{
		State: "open",
		Pulls: []store.PullListItem{
			{ID: 1, Number: 1, Title: "ready", State: "open", RepoFullName: "a/r"},
			{ID: 2, Number: 2, Title: "wip", State: "open", IsDraft: true, RepoFullName: "a/r"},
		},
	}))
	if row := rowFor(t, html, "a/r ready"); !strings.Contains(row, ">Open</span>") || strings.Contains(row, "Draft") {
		t.Errorf("ready PR should show Open, not Draft: %s", row)
	}
	if row := rowFor(t, html, "a/r wip"); !strings.Contains(row, "Draft") || strings.Contains(row, ">Open</span>") {
		t.Errorf("draft PR should show Draft, not Open: %s", row)
	}
}

func TestAccountIssues_MarksPrivate(t *testing.T) {
	html := renderPage(t, pages.AccountIssues(view.AccountIssuesData{
		State: "open",
		Issues: []store.IssueListItem{
			{ID: 1, Number: 1, Title: "public", State: "open", Visibility: "public", RepoFullName: "a/r"},
			{ID: 2, Number: 2, Title: "secret", State: "open", Visibility: "private", RepoFullName: "a/r"},
		},
	}))
	if row := rowFor(t, html, "a/r public"); strings.Contains(row, `aria-label="Private"`) {
		t.Errorf("public issue marked private: %s", row)
	}
	if row := rowFor(t, html, "a/r secret"); !strings.Contains(row, `aria-label="Private"`) {
		t.Errorf("private issue not marked: %s", row)
	}
}

func TestReleases_ShowsExcerptNotBody(t *testing.T) {
	html := renderPage(t, pages.Releases(view.ReleasesData{
		Releases: []view.ReleaseView{{
			Release: model.Release{TagName: "v1", Body: "## Raw **markdown**"},
			Excerpt: "Raw markdown",
		}},
	}))
	if !strings.Contains(html, `line-clamp-2">Raw markdown</p>`) || strings.Contains(html, "## Raw") {
		t.Errorf("release card should show the clamped excerpt only")
	}
}

func TestClientFilteredLists_HaveNoMatchRow(t *testing.T) {
	for name, tc := range map[string]struct {
		page templ.Component
		want string
	}{
		"issues":    {pages.AccountIssues(view.AccountIssuesData{Issues: []store.IssueListItem{{ID: 1}}}), "No issues match this filter."},
		"pulls":     {pages.AccountPulls(view.AccountPullsData{Pulls: []store.PullListItem{{ID: 1}}}), "No pull requests match this filter."},
		"repos":     {pages.AccountRepos(view.AccountReposData{Repos: []model.Repository{{ID: 1}}}), "No repositories match this filter."},
		"attention": {pages.AccountAttention(view.AttentionData{Items: []service.AttentionItem{{}}}), "No items match this filter."},
		"gists":     {pages.Gists(view.GistsData{Gists: []view.GistListItem{{}}}), "No gists match this filter."},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(renderPage(t, tc.page), tc.want) {
				t.Errorf("lacks %q", tc.want)
			}
		})
	}
}
