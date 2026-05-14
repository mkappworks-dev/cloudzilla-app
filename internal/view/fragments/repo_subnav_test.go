package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestRepoSubnav_RendersAllTabs(t *testing.T) {
	repo := &model.Repository{Name: "demo"}
	data := RepoSubnavData{
		OwnerName: "alice",
		Repo:      repo,
		Active:    "code",
	}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, tab := range []string{"Code", "Issues", "Pull requests", "Actions", "Discussions", "Projects", "Wiki", "Releases", "Settings"} {
		if !strings.Contains(out, tab) {
			t.Errorf("expected %q in output, got: %s", tab, out)
		}
	}
}

func TestRepoSubnav_MarksActiveTab(t *testing.T) {
	repo := &model.Repository{Name: "demo"}
	data := RepoSubnavData{OwnerName: "alice", Repo: repo, Active: "pull_requests"}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), `aria-current="page"`) {
		t.Errorf("expected aria-current on active tab; got: %s", buf.String())
	}
}

func TestRepoSubnav_RendersCountBadges(t *testing.T) {
	repo := &model.Repository{Name: "demo"}
	data := RepoSubnavData{
		OwnerName: "alice",
		Repo:      repo,
		Active:    "code",
		Counts: map[string]int{
			"issues":        23,
			"pull_requests": 12,
			"discussions":   5,
			"releases":      3,
		},
	}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, n := range []string{"23", "12", "5", "3"} {
		if !strings.Contains(out, ">"+n+"<") {
			t.Errorf("expected count badge %q in output", n)
		}
	}
}

func TestRepoSubnav_OmitsZeroAndMissingCounts(t *testing.T) {
	repo := &model.Repository{Name: "demo"}
	data := RepoSubnavData{
		OwnerName: "alice",
		Repo:      repo,
		Active:    "code",
		Counts:    map[string]int{"issues": 0},
	}
	var buf bytes.Buffer
	if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	// A zero count must not render a badge (mockup shows badges only for non-zero values).
	if strings.Contains(buf.String(), `aria-label="0`) {
		t.Errorf("did not expect zero-count badge; got: %s", buf.String())
	}
}
