package pages_test

import (
	"context"
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

// Overview tab on own profile must surface pinned repos, the contributions
// heatmap, and the Edit-profile entry point.
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

// On someone else's profile, the Edit-profile button must not render.
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

// Repositories tab must render the repo table; overview-only sections like the
// heatmap heading must not appear.
func TestUser_RepositoriesTabShowsTable(t *testing.T) {
	data := view.UserData{
		User: model.User{ID: 1, Username: "alice"},
		Tab:  "repositories",
		Repos: []model.Repository{
			{ID: 1, OwnerName: "alice", Name: "demo", UpdatedAt: time.Now()},
		},
	}
	out := renderUser(t, data)

	if !strings.Contains(out, "demo") {
		t.Errorf("repo table missing repo name\n--- output ---\n%s", out)
	}
	if strings.Contains(out, "contributions in the last year") {
		t.Errorf("repositories tab should not render the heatmap heading\n--- output ---\n%s", out)
	}
}
