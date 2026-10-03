package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// layout.Base appends " — Cloudzilla" itself, so a page title that names the
// brand shows it twice in the browser tab.
func TestPageTitles_NameCloudzillaOnce(t *testing.T) {
	for name, page := range map[string]templ.Component{
		"AdminSettings":  pages.AdminSettings(view.AdminSettingsData{}),
		"AuditLog":       pages.AuditLog(view.AuditLogData{Page: 1, PerPage: 50}),
		"SSOSettings":    pages.SSOSettings(view.SSOSettingsData{}),
		"OAuthAuthorize": pages.OAuthAuthorize(view.OAuthAuthorizeData{App: model.OAuthApp{Name: "CI Bot"}}),
		"ReleaseDetail":  pages.ReleaseDetail(view.ReleaseDetailData{RepoName: "rocket", Release: model.Release{TagName: "v1.0.0"}}),
		"Setup":          pages.Setup(view.SetupData{}),
		"WikiPage":       pages.WikiPage(view.WikiPageData{RepoName: "rocket", Slug: "Home"}),
		"WikiNew":        pages.WikiNew(view.WikiNewData{RepoName: "rocket"}),
		"WikiEdit":       pages.WikiEdit(view.WikiEditData{RepoName: "rocket", Slug: "Home"}),
	} {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			if err := page.Render(context.Background(), &sb); err != nil {
				t.Fatalf("render: %v", err)
			}
			title := documentTitle(t, sb.String())
			if n := strings.Count(title, "Cloudzilla"); n != 1 {
				t.Errorf("<title> %q names Cloudzilla %d times, want 1", title, n)
			}
		})
	}
}

// documentTitle returns the first <title>, the document's: inline SVG icons
// later in the body can carry their own.
func documentTitle(t *testing.T, html string) string {
	t.Helper()
	_, rest, ok := strings.Cut(html, "<title>")
	if !ok {
		t.Fatal("page has no <title>")
	}
	title, _, ok := strings.Cut(rest, "</title>")
	if !ok {
		t.Fatal("<title> is not closed")
	}
	return title
}
