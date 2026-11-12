package pages_test

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// rowFor returns the rendered <li> that holds title.
func rowFor(t *testing.T, html, title string) string {
	t.Helper()
	i := strings.Index(html, ">"+title+"<")
	if i < 0 {
		t.Fatalf("no row %q", title)
	}
	start := strings.LastIndex(html[:i], "<li")
	return html[start : i+strings.Index(html[i:], "</li>")]
}

func TestAccountPulls_MarksDrafts(t *testing.T) {
	html := renderPage(t, pages.AccountPulls(view.AccountPullsData{
		Pulls: []store.PullListItem{
			{ID: 1, Number: 1, Title: "ready", State: "open", RepoFullName: "a/r"},
			{ID: 2, Number: 2, Title: "wip", State: "open", IsDraft: true, RepoFullName: "a/r"},
		},
	}))
	if row := rowFor(t, html, "ready"); !strings.Contains(row, ">Open</span>") || strings.Contains(row, "Draft") {
		t.Errorf("ready PR should show Open, not Draft: %s", row)
	}
	if row := rowFor(t, html, "wip"); !strings.Contains(row, "Draft") || strings.Contains(row, ">Open</span>") {
		t.Errorf("draft PR should show Draft, not Open: %s", row)
	}
}

func TestAccountIssues_MarksPrivate(t *testing.T) {
	html := renderPage(t, pages.AccountIssues(view.AccountIssuesData{
		Issues: []store.IssueListItem{
			{ID: 1, Number: 1, Title: "public", State: "open", Visibility: "public", RepoFullName: "a/r"},
			{ID: 2, Number: 2, Title: "secret", State: "open", Visibility: "private", RepoFullName: "a/r"},
		},
	}))
	if row := rowFor(t, html, "public"); strings.Contains(row, `aria-label="Private"`) {
		t.Errorf("public issue marked private: %s", row)
	}
	if row := rowFor(t, html, "secret"); !strings.Contains(row, `aria-label="Private"`) {
		t.Errorf("private issue not marked: %s", row)
	}
}

func TestReleases_ShowsExcerptNotBody(t *testing.T) {
	html := renderPage(t, pages.Releases(view.ReleasesData{
		Releases: []view.ReleaseView{{
			Release: model.Release{TagName: "v1", Body: "## Raw **markdown**"},
			Excerpt: []string{"What's new", "Raw markdown"},
		}},
	}))
	if !strings.Contains(html, `truncate">What&#39;s new</p><p class="truncate">Raw markdown</p>`) || strings.Contains(html, "## Raw") {
		t.Errorf("release card should show one line per excerpt block, not the body")
	}
}

func TestClientFilteredLists_HaveNoMatchRow(t *testing.T) {
	for name, tc := range map[string]struct {
		page templ.Component
		want string
	}{
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

func TestAccountLists_FilteredEmptyOffersClear(t *testing.T) {
	nav := view.AccountListNav{
		StateFilter: "open",
		ClearURL:    "/issues?q=",
		Filter:      components.ListFilterData{HasFilters: true, Action: "/issues"},
	}
	for name, page := range map[string]templ.Component{
		"issues": pages.AccountIssues(view.AccountIssuesData{AccountListNav: nav}),
		"pulls":  pages.AccountPulls(view.AccountPullsData{AccountListNav: nav}),
	} {
		t.Run(name, func(t *testing.T) {
			html := renderPage(t, page)
			if !strings.Contains(html, "match these filters.") || !strings.Contains(html, `href="/issues?q="`) {
				t.Errorf("a filtered empty list should say so and link back: %s", html)
			}
		})
	}
}
