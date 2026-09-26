package pages_test

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func renderUser(t *testing.T, data view.UserData) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.User(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestUser_OverviewOwnProfile(t *testing.T) {
	data := view.UserData{
		User: model.User{
			ID:        42,
			Username:  "alice",
			CreatedAt: time.Date(2024, 8, 15, 0, 0, 0, 0, time.UTC),
		},
		IsOwnProfile: true,
		Tab:          "overview",
		PinnedRepos: []components.PinnedRepoData{
			{OwnerName: "alice", Name: "cloudzilla", Description: "self-hosted git", Language: "Go", LanguageColor: "#00ADD8", Stars: 12},
		},
		Heatmap: map[time.Time]int{time.Now().Truncate(24 * time.Hour): 3},
		RecentActivity: []model.Event{
			{ID: 1, ActorName: "alice", OwnerName: "alice", RepoName: "cloudzilla", EventType: model.EventPROpened, CreatedAt: time.Now().Add(-2 * time.Hour)},
		},
	}
	out := renderUser(t, data)

	for _, want := range []string{
		"Pinned",
		"cloudzilla",
		"Edit profile",
		"contributions in the last year",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("overview missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestUser_OverviewOtherProfile(t *testing.T) {
	data := view.UserData{
		User: model.User{
			ID:        7,
			Username:  "bob",
			CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		IsOwnProfile: false,
		Tab:          "overview",
		Heatmap:      map[time.Time]int{},
	}
	out := renderUser(t, data)

	if strings.Contains(out, "Edit profile") {
		t.Errorf("Edit profile must not render on someone else's profile\n--- output ---\n%s", out)
	}
}

func TestUser_RepositoriesTabShowsTable(t *testing.T) {
	data := view.UserData{
		User: model.User{ID: 1, Username: "alice"},
		Tab:  "repositories",
		RepoTabRepos: []model.Repository{
			{ID: 1, OwnerName: "alice", Name: "demo", UpdatedAt: time.Now()},
		},
	}
	out := renderUser(t, data)

	if !strings.Contains(out, "demo") {
		t.Errorf("repo list missing repo name\n--- output ---\n%s", out)
	}
	if strings.Contains(out, "contributions in the last year") {
		t.Errorf("repositories tab should not render the heatmap heading\n--- output ---\n%s", out)
	}
}

// The hover accent bar is built from utilities; a revert to the deleted .row-card rule would render unstyled rows.
func TestUser_RowCardsUseTailwindUtilities(t *testing.T) {
	repo := model.Repository{ID: 1, OwnerName: "alice", Name: "demo", UpdatedAt: time.Now()}
	for _, tc := range []struct {
		tab  string
		data view.UserData
		href string
	}{
		{"repositories", view.UserData{RepoTabRepos: []model.Repository{repo}}, "/alice/demo"},
		{"stars", view.UserData{StarredRepos: []model.Repository{repo}}, "/alice/demo"},
		{"gists", view.UserData{GistsTabItems: []view.GistTabItem{{Gist: model.Gist{ID: "g1", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}}, "/gists/g1"},
	} {
		tc.data.User = model.User{ID: 1, Username: "alice"}
		tc.data.Tab = tc.tab
		out := renderUser(t, tc.data)

		m := regexp.MustCompile(`<a href="` + regexp.QuoteMeta(tc.href) + `" class="([^"]*)"`).FindStringSubmatch(out)
		if m == nil {
			t.Errorf("%s tab: no row anchor for %s", tc.tab, tc.href)
			continue
		}
		classes := strings.Fields(m[1])
		for _, want := range []string{"hover:pl-[18px]", "motion-reduce:hover:pl-4", "before:bg-foreground", "hover:before:opacity-100"} {
			if !slices.Contains(classes, want) {
				t.Errorf("%s tab: row anchor lacks %q (classes %q)", tc.tab, want, m[1])
			}
		}
		if slices.Contains(classes, "row-card") {
			t.Errorf("%s tab: row anchor still uses the removed row-card class", tc.tab)
		}
	}
}
