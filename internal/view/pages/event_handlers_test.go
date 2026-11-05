package pages_test

import (
	"html"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// templ replaces a string given to an on*/hx-on* attribute with a comment
// instead of failing, so a handler passed as a plain string disappears silently.
func TestEventHandlerAttributes_SurviveSanitization(t *testing.T) {
	signedIn := view.BasePage{CurrentUser: &middleware.Claims{UserID: 1, Username: "alice"}}
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
		{
			"issue comment form",
			render(t, pages.IssueDetail(view.IssueDetailData{
				BasePage: signedIn, Owner: "alice", RepoName: "r", CanWrite: true, CanManage: true,
				Issue: model.Issue{State: "open"},
			})),
			"issue-comment-count",
		},
		{
			"issue close dialog",
			render(t, pages.IssueDetail(view.IssueDetailData{
				BasePage: signedIn, Owner: "alice", RepoName: "r", CanWrite: true, CanManage: true,
				Issue: model.Issue{State: "open"},
			})),
			"close-issue-dialog",
		},
		{
			"issue reopen reloads",
			render(t, pages.IssueDetail(view.IssueDetailData{
				BasePage: signedIn, Owner: "alice", RepoName: "r", CanWrite: true, CanManage: true,
				Issue: model.Issue{State: "closed"},
			})),
			"window.location.reload()",
		},
		{
			"pull reopen toast",
			render(t, pages.PullDetail(view.PullDetailData{
				Owner: "alice", RepoName: "r", CanWrite: true, Pull: model.PullRequest{State: model.PRStateClosed},
			})),
			"Pull request reopened",
		},
		{
			"pull comment form resets",
			render(t, pages.PullDetail(view.PullDetailData{
				BasePage: signedIn, Owner: "alice", RepoName: "r", CanWrite: true, Pull: model.PullRequest{State: model.PRStateOpen},
			})),
			"this.reset()",
		},
		{
			"pull close dialog",
			render(t, pages.PullDetail(view.PullDetailData{
				BasePage: signedIn, Owner: "alice", RepoName: "r", CanWrite: true, Pull: model.PullRequest{State: model.PRStateOpen},
			})),
			"document.getElementById('close-pull-dialog').showModal()",
		},
		{
			"discussion reply form resets",
			render(t, pages.DiscussionDetail(view.DiscussionDetailData{
				BasePage: signedIn, Owner: "alice", RepoName: "r", CanWrite: true,
			})),
			"this.reset()",
		},
		{
			"discussion reply mark answer",
			renderReplyFragment(t, view.RenderedDiscussionReply{}, 1, true),
			"window.location.reload()",
		},
		{
			"organizations leave",
			render(t, pages.Organizations(view.OrgListData{
				BasePage: signedIn,
				Entries:  []view.OrgListEntry{{Org: model.Organization{ID: 1, Name: "acme"}, Role: model.OrgRoleMember}},
			})),
			"window.location.reload()",
		},
		{
			"project dialog cancel",
			render(t, pages.ProjectDetail(boardData(true))),
			"this.closest('dialog').close()",
		},
		{
			"sso provider switch",
			render(t, pages.SSOSettings(view.SSOSettingsData{
				BasePage:   signedIn,
				LDAPConfig: &model.SSOConfig{Provider: "ldap", Enabled: true, Config: map[string]string{}},
			})),
			"this.form.requestSubmit()",
		},
		{
			"repo transfer accept toast",
			render(t, pages.RepoTransfers(view.RepoTransfersData{
				BasePage:  signedIn,
				Transfers: []view.IncomingRepoTransfer{{RepoTransfer: model.RepoTransfer{ID: 1, OwnerName: "bob", RepoName: "r", RecipientName: "alice"}}},
			})),
			"Repository transferred to you",
		},
		{
			"command palette theme toggle",
			render(t, pages.Organizations(view.OrgListData{BasePage: signedIn})),
			"document.getElementById('theme-toggle').click()",
		},
		{
			"admin user delete toast",
			render(t, pages.AdminUser(view.AdminUserData{BasePage: signedIn, Detail: service.AdminUserDetail{User: model.User{ID: 2, Username: "bob"}}})),
			"Account deleted",
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
