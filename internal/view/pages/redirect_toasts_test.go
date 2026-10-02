package pages_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

var afterRequestAttr = regexp.MustCompile(`hx-on::after:request="([^"]*)"`)

// These controls are answered with HX-Redirect or HX-Refresh, so their toasts
// must be stashed for the next page, and only when that header arrives: form
// errors come back as 200 swaps.
func TestRedirectingControls_StashTheirToast(t *testing.T) {
	repoSettings := func(archived, template bool) string {
		return render(t, pages.RepoSettings(view.RepoSettingsData{
			Repo:     model.Repository{Name: "r", IsArchived: archived, IsTemplate: template},
			Owner:    "alice",
			RepoName: "r",
			IsOwner:  true,
		}))
	}
	orgSettings := render(t, pages.OrgSettings(view.OrgSettingsData{Org: model.Organization{ID: 1, Name: "acme"}, MemberCount: 1}))

	for _, tc := range []struct{ page, request, toast string }{
		{repoSettings(false, false), `hx-post="/api/repos/alice/r/archive"`, "Repository archived"},
		{repoSettings(true, false), `hx-post="/api/repos/alice/r/unarchive"`, "Repository unarchived"},
		{repoSettings(false, false), `hx-patch="/api/repos/alice/r/template"`, "Repository is now a template"},
		{repoSettings(false, true), `hx-patch="/api/repos/alice/r/template"`, "Repository is no longer a template"},
		{repoSettings(false, false), `hx-post="/api/repos/alice/r/delete"`, "Repository deleted"},
		{orgSettings, `hx-post="/api/orgs/acme/transfer"`, "Organization transferred"},
		{orgSettings, `hx-post="/api/orgs/acme/delete"`, "Organization deleted"},
	} {
		tag := regexp.MustCompile(`<[^>]*` + regexp.QuoteMeta(tc.request) + `[^>]*>`).FindString(tc.page)
		if tag == "" {
			t.Errorf("%s: not rendered", tc.request)
			continue
		}
		m := afterRequestAttr.FindStringSubmatch(tag)
		if m == nil {
			t.Errorf("%s: no hx-on::after:request to stash %q", tc.request, tc.toast)
			continue
		}
		on := html.UnescapeString(m[1])
		for _, want := range []string{"ctx.hx.redirect", "event.target===this", jsonLit(t, tc.toast)} {
			if !strings.Contains(on, want) {
				t.Errorf("%s: after:request handler lacks %s: %s", tc.request, want, on)
			}
		}
	}
}
