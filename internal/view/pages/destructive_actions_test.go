package pages_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const filledDestructive = "bg-destructive text-destructive-foreground"

var dialogsAndScripts = regexp.MustCompile(`(?s)<(dialog|script).*?</(dialog|script)>`)

func settingsPage(t *testing.T, private bool) string {
	t.Helper()
	return render(t, pages.RepoSettings(view.RepoSettingsData{
		Repo:      model.Repository{Name: "r", Private: private},
		Owner:     "alice",
		RepoName:  "r",
		IsOwner:   true,
		CanManage: true,
		Confirm:   components.ConfirmFactors{Password: true},
	}))
}

func dialogOf(t *testing.T, page, id string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<dialog id="` + id + `".*?</dialog>`).FindString(page)
	if m == "" {
		t.Fatalf("no dialog %q", id)
	}
	return m
}

// Archive, delete and a visibility change each ask for the repository name in a labelled field.
func TestRepoSettings_DestructiveDialogsAskForTheName(t *testing.T) {
	for _, private := range []bool{false, true} {
		page := settingsPage(t, private)
		for id, field := range map[string]string{
			"archive-dialog":     "archive-confirm-name",
			"delete-repo-dialog": "delete-repo-confirm-name",
			"visibility-dialog":  "visibility-confirm-name",
		} {
			dlg := dialogOf(t, page, id)
			for _, want := range []string{`<label for="` + field + `"`, `id="` + field + `"`, `name="confirm_name"`, ` required`} {
				if !strings.Contains(dlg, want) {
					t.Errorf("%s (private=%v) lacks %s", id, private, want)
				}
			}
		}
	}
}

// Going public hands the code to everyone, so only that direction also takes the password.
func TestRepoSettings_VisibilityDialogPasswordOnlyForMakingPublic(t *testing.T) {
	toPublic := dialogOf(t, settingsPage(t, true), "visibility-dialog")
	if !strings.Contains(toPublic, `name="password"`) || !strings.Contains(toPublic, `name="private" value="false"`) {
		t.Errorf("making public should ask for the password and post private=false:\n%s", toPublic)
	}
	toPrivate := dialogOf(t, settingsPage(t, false), "visibility-dialog")
	if strings.Contains(toPrivate, `name="password"`) || !strings.Contains(toPrivate, `name="private" value="true"`) {
		t.Errorf("making private should post private=true without a password:\n%s", toPrivate)
	}
}

// The buttons that open the dialogs are outline, not filled; the filled one is the dialog's own submit.
func TestRepoSettings_DangerRowsAreNotFilled(t *testing.T) {
	page := settingsPage(t, false)
	for _, id := range []string{"archive-dialog", "delete-repo-dialog", "visibility-dialog"} {
		opener := regexp.MustCompile(`<button[^>]*getElementById\(&#39;` + id + `&#39;\)[^>]*>`).FindString(page)
		if opener == "" {
			t.Errorf("nothing opens %s", id)
		} else if strings.Contains(opener, filledDestructive) {
			t.Errorf("the opener of %s is filled: %s", id, opener)
		}
	}
	if strings.Contains(page, `aria-label="Repository visibility"`) {
		t.Error("the visibility toggle is still on the page")
	}
	if !strings.Contains(page, `id="visibility"`) || !strings.Contains(page, `href="#visibility"`) {
		t.Error("visibility has no section or sidebar entry")
	}
}

func TestRepoSettings_VisibilityIsForManagersAndOthersSeeNothing(t *testing.T) {
	page := render(t, pages.RepoSettings(view.RepoSettingsData{
		Repo: model.Repository{Name: "r"}, Owner: "alice", RepoName: "r",
	}))
	if strings.Contains(page, "visibility-dialog") || strings.Contains(page, "archive-dialog") {
		t.Error("a viewer without manage rights is offered a danger dialog")
	}
}

func TestRowDeleteButtons_AreQuietIcons(t *testing.T) {
	branches := render(t, fragments.BranchesList(view.BranchesFragData{
		Owner: "alice", RepoName: "r", CanWrite: true, DefaultBranch: "main",
		Branches: []service.BranchInfo{{Name: "main", IsDefault: true}, {Name: "topic"}},
	}))
	tags := render(t, fragments.TagsList(view.TagsFragData{
		Owner: "alice", RepoName: "r", CanWrite: true, Tags: []service.TagInfo{{Name: "v1"}},
	}))
	projects := render(t, pages.Projects(view.ProjectsData{
		Owner: "alice", RepoName: "r", CanWrite: true, StateFilter: "open", OpenCount: 1,
		Items: []service.ProjectListView{{Project: model.Project{ID: 3, Name: "Roadmap"}}},
	}))
	for name, tc := range map[string]struct{ out, label string }{
		"branch":  {branches, `aria-label="Delete branch topic"`},
		"tag":     {tags, `aria-label="Delete tag v1"`},
		"project": {projects, `aria-label="Delete project Roadmap"`},
	} {
		if !strings.Contains(tc.out, tc.label) {
			t.Errorf("%s: no %s", name, tc.label)
		}
		for _, loud := range []string{filledDestructive, "border-destructive/40", ">Delete<"} {
			if strings.Contains(dialogsAndScripts.ReplaceAllString(tc.out, ""), loud) {
				t.Errorf("%s row still has %q", name, loud)
			}
		}
		if !strings.Contains(tc.out, "<svg") {
			t.Errorf("%s: delete is not an icon", name)
		}
	}
}

func TestAdminUser_SuspendAndRevokeAreNotFilled(t *testing.T) {
	out := render(t, pages.AdminUser(view.AdminUserData{
		BasePage: view.BasePage{CurrentUser: &middleware.Claims{UserID: 1, Username: "alice"}},
		Detail:   service.AdminUserDetail{User: model.User{ID: 2, Username: "bob"}},
	}))
	for _, label := range []string{"Suspend", "Revoke"} {
		m := regexp.MustCompile(`<button[^>]*data-confirm-label="` + label + `"[^>]*>`).FindString(out)
		if m == "" {
			t.Fatalf("no %s button", label)
		}
		if strings.Contains(m, filledDestructive) {
			t.Errorf("%s button is filled: %s", label, m)
		}
	}
}
