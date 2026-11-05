package pages_test

import (
	"html"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// templ replaces a string given to an on*/hx-on* attribute with a comment
// instead of failing, so a handler passed as a plain string disappears silently.
func TestEventHandlerAttributes_SurviveSanitization(t *testing.T) {
	for _, tc := range []struct{ name, page, handler string }{
		{
			"projects new button",
			render(t, pages.Projects(view.ProjectsData{Owner: "alice", RepoName: "r", CanWrite: true})),
			"document.getElementById('new-project-dialog').showModal()",
		},
		{
			"project delete reloads",
			render(t, pages.Projects(view.ProjectsData{
				Owner: "alice", RepoName: "r", CanWrite: true, OpenCount: 1, StateFilter: "open",
				Items: []service.ProjectListView{{Project: model.Project{ID: 1, Name: "p"}}},
			})),
			"window.location.reload()",
		},
		{
			"gist delete",
			render(t, pages.GistDetail(view.GistDetailData{IsOwner: true, Gist: model.Gist{ID: "g1"}})),
			"window.location.href='/gists'",
		},
		{
			"blob delete dialog",
			render(t, pages.Blob(view.BlobData{Owner: "alice", RepoName: "r", Path: "a.txt", CanDelete: true})),
			"document.getElementById('blob-delete-dialog').showModal()",
		},
		{
			"user pin dialog",
			render(t, pages.User(view.UserData{IsOwnProfile: true, Tab: "overview"})),
			"document.getElementById('pin-modal').showModal()",
		},
		{
			"wiki delete toast",
			render(t, pages.WikiPage(view.WikiPageData{Owner: "alice", RepoName: "r", Slug: "Home", CanManage: true, Exists: true})),
			"Wiki page deleted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.page, "failed sanitization") {
				t.Error("page contains a templ sanitization placeholder")
			}
			if !strings.Contains(html.UnescapeString(tc.page), tc.handler) {
				t.Errorf("handler %q not rendered", tc.handler)
			}
		})
	}
}
