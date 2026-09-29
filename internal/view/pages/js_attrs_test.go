package pages_test

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func jsonLit(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var ownerSelectXData = regexp.MustCompile(`x-data="([^"]*)"[^>]*>\s*<input type="hidden" id="repo-owner"`)

func TestRepoNew_OwnerNameCannotBreakOutOfXData(t *testing.T) {
	for _, name := range testutil.HostileNames {
		out := render(t, pages.RepoNew(view.RepoNewData{
			BasePage:     view.BasePage{CurrentUser: &middleware.Claims{UserID: 1, Username: "alice"}},
			OwnedOrgs:    []model.Organization{{ID: 2, Name: name, DefaultRepoVisibility: "public"}},
			DefaultOwner: name,
		}))
		testutil.AssertNameStaysQuoted(t, out, name)

		m := ownerSelectXData.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("owner select x-data not found")
		}
		want := "{ open: false, value: " + jsonLit(t, name) + ", label: " + jsonLit(t, name) + " }"
		if got := html.UnescapeString(m[1]); got != want {
			t.Errorf("owner x-data = %s, want %s", got, want)
		}
	}
}

func TestRepo_CloneURLsStayQuoted(t *testing.T) {
	for _, name := range testutil.HostileNames {
		out := render(t, pages.Repo(view.RepoData{
			Owner:     name,
			RepoName:  "r",
			CloneHTTP: "http://localhost/" + name + "/r.git",
			CloneSSH:  "git@localhost:" + name + "/r.git",
		}))
		testutil.AssertNameStaysQuoted(t, out, name)
	}
}

func TestPullNew_ReviewerNamesStayQuoted(t *testing.T) {
	for _, name := range testutil.HostileNames {
		out := render(t, pages.PullNew(view.PullNewData{
			Owner:    "alice",
			RepoName: "r",
			Branches: []service.BranchInfo{{Name: "main"}},
			Reviewer: components.ReviewerPickerData{All: []components.ReviewerOption{{Username: name, Selected: true}}},
		}))
		testutil.AssertNameStaysQuoted(t, out, name)
		if !strings.Contains(html.UnescapeString(out), "reviewers: ["+jsonLit(t, name)+"]") {
			t.Errorf("preselected reviewers not rendered as a JSON list for %q", name)
		}
	}
}

func TestPullNew_NoPreselectedReviewersIsEmptyList(t *testing.T) {
	out := render(t, pages.PullNew(view.PullNewData{
		Owner:    "alice",
		RepoName: "r",
		Branches: []service.BranchInfo{{Name: "main"}},
		Reviewer: components.ReviewerPickerData{All: []components.ReviewerOption{{Username: "bob"}}},
	}))
	if !strings.Contains(out, "reviewers: [],") {
		t.Error("reviewers must initialise to [] so .includes works")
	}
}

func TestIssueNew_AssigneeNamesStayQuoted(t *testing.T) {
	for _, name := range testutil.HostileNames {
		out := render(t, pages.IssueNew(view.IssueNewData{
			Owner:         "alice",
			RepoName:      "r",
			ShowForm:      true,
			CanWrite:      true,
			Collaborators: []string{name},
		}))
		testutil.AssertNameStaysQuoted(t, out, name)
	}
}

func TestWikiPage_DeleteRedirectKeepsOwnerQuoted(t *testing.T) {
	for _, name := range testutil.HostileNames {
		out := render(t, pages.WikiPage(view.WikiPageData{
			Owner:     name,
			RepoName:  "r",
			Slug:      "Home",
			CanManage: true,
			Exists:    true,
		}))
		testutil.AssertNameStaysQuoted(t, out, name)
	}
}

func TestUser_ShareButtonKeepsUsernameQuoted(t *testing.T) {
	for _, name := range testutil.HostileNames {
		out := render(t, pages.User(view.UserData{
			User:         model.User{ID: 1, Username: name},
			IsOwnProfile: true,
			Tab:          "overview",
		}))
		testutil.AssertNameStaysQuoted(t, out, name)
	}
}

func TestOrg_NewRepoLinkEscapesOrgName(t *testing.T) {
	data := orgFixture()
	data.Org.Name = "a&owner=b"
	data.CanManage = true
	out := renderOrg(t, data)
	if !strings.Contains(out, `href="/repos/new?owner=a%26owner%3Db"`) {
		t.Error("new-repo link must query-escape the org name")
	}
}
