package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func renderOrg(t *testing.T, data view.OrgData) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.Org(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func orgFixture() view.OrgData {
	return view.OrgData{
		Org: model.Organization{ID: 1, Name: "acme", DisplayName: "Acme Inc"},
		Repos: []model.Repository{
			{ID: 1, OwnerName: "acme", Name: "rocket"},
		},
		Members: []model.OrgMember{
			{ID: 1, OrgID: 1, UserID: 7, Username: "alice", Role: model.OrgRoleOwner},
		},
		MemberCount: 1,
		RecentRepos: []components.PinnedRepoData{{OwnerName: "acme", Name: "rocket"}},
	}
}

func TestOrg_RendersHeaderAndSections(t *testing.T) {
	out := renderOrg(t, orgFixture())

	for _, want := range []string{
		"Acme Inc",
		"@acme",
		"acme/rocket",
		"alice",
		"Recently updated",
		"People",
		`href="/acme?tab=repositories"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("org detail page missing %q", want)
		}
	}
}

// ?tab=repositories is the only place an org's full repo list is reachable,
// so it must swap the "Recently updated" teaser for the full list.
func TestOrg_ShowAllRepos(t *testing.T) {
	data := orgFixture()
	data.ShowAllRepos = true
	out := renderOrg(t, data)

	if !strings.Contains(out, `>Repositories</h2>`) {
		t.Error("repositories view missing Repositories heading")
	}
	if strings.Contains(out, "Recently updated") {
		t.Error("repositories view still shows the Recently updated teaser")
	}
	if !strings.Contains(out, "acme/rocket") {
		t.Error("repositories view missing repo")
	}
}

// Websites saved before validation existed can still hold a javascript: URL.
func TestOrg_UnsafeWebsiteNotLinked(t *testing.T) {
	data := orgFixture()
	data.Org.Website = "javascript:alert(1)"
	out := renderOrg(t, data)

	if strings.Contains(out, `href="javascript:`) {
		t.Error("org page links a javascript: website")
	}
}
