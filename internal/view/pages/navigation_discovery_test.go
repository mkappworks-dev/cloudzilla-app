package pages_test

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestSearchTabs_CodeIsAPeer(t *testing.T) {
	general := render(t, pages.Search(view.SearchData{Query: "needle", Type: "all"}))
	code := render(t, pages.CodeSearch(view.CodeSearchData{Query: "needle", Page: 1}))

	for name, out := range map[string]string{"search": general, "code search": code} {
		for _, want := range []string{">Repositories", ">Users", `href="/search/code?q=needle"`, `href="/search?q=needle&amp;type=repos"`} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: tab bar missing %q", name, want)
			}
		}
	}
	if strings.Contains(general, `aria-current="page">Code`) {
		t.Error("Code must not be current on the general search page")
	}
	if !strings.Contains(code, "Code") || !strings.Contains(code, `aria-current="page"`) {
		t.Error("Code must be the current tab on the code search page")
	}
}

func TestRepoSettings_DefaultBranchIsAPickerOfExistingBranches(t *testing.T) {
	page := func(branches []string) string {
		return render(t, pages.RepoSettings(view.RepoSettingsData{
			Repo:      model.Repository{Name: "r", DefaultBranch: "main"},
			Owner:     "alice",
			RepoName:  "r",
			Branches:  branches,
			CanManage: true,
			Confirm:   components.ConfirmFactors{Password: true},
		}))
	}

	withBranches := page([]string{"dev", "main"})
	if !strings.Contains(withBranches, `<select id="repo-default-branch" name="default_branch"`) {
		t.Fatal("default branch is not a select")
	}
	if !strings.Contains(withBranches, `<option value="main" selected>`) || !strings.Contains(withBranches, `<option value="dev">`) {
		t.Error("options should list the branches and select the current default")
	}

	empty := page(nil)
	if !strings.Contains(empty, `<input id="repo-default-branch" type="text"`) {
		t.Error("an empty repository keeps the text input: the first push creates the branch")
	}
}

func TestExplore_FormKeepsFiltersAcrossTabsAndPages(t *testing.T) {
	out := render(t, pages.Explore(view.ExploreData{
		Tab: "newest", Query: "kube", Language: "Go", Languages: []string{"Go", "Rust"},
		Page: 2, Total: 60, PageSize: 25,
		Repos: []model.RepositoryWithStats{{Repository: model.Repository{OwnerName: "a", Name: "b"}}},
	}))
	for _, want := range []string{
		`value="kube"`,
		`<option value="Go" selected>`,
		`/explore?language=Go&amp;q=kube&amp;tab=forked`,
		`/explore?language=Go&amp;page=3&amp;q=kube&amp;tab=newest`,
		`/explore?language=Go&amp;q=kube&amp;tab=newest"`,
		"Clear filters",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("explore page missing %q", want)
		}
	}
}

func TestForgotPassword_WithoutEmailLinksAdministrators(t *testing.T) {
	out := render(t, pages.ForgotPassword(view.ForgotPasswordData{Admins: []string{"root", "ada"}}))
	for _, want := range []string{"Ask an administrator", `href="/root"`, `href="/ada"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	none := render(t, pages.ForgotPassword(view.ForgotPasswordData{}))
	if !strings.Contains(none, "cz-admin password-reset-link") {
		t.Error("with no administrator to name, the page should point at the command line")
	}
}
