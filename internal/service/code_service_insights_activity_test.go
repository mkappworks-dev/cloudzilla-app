package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type insightsCommit struct {
	when   time.Time
	author string
	email  string
	lines  int
}

// A commit with lines = n rewrites f.txt to hold n lines, so its diff against the previous commit is exactly the line-count difference.
func newInsightsRepo(t *testing.T, owner, name string, commits ...insightsCommit) *CodeService {
	t.Helper()
	root := t.TempDir()
	bareDir := filepath.Join(root, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.InitBareRepo(t, bareDir)
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})
	if len(commits) == 0 {
		return svc
	}

	workDir := t.TempDir()
	work, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := work.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commits {
		content := strings.Repeat("line\n", c.lines)
		if err := os.WriteFile(filepath.Join(workDir, "f.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add("f.txt"); err != nil {
			t.Fatal(err)
		}
		sig := &gitobj.Signature{Name: c.author, Email: c.email, When: c.when}
		if _, err := wt.Commit("c", &gogit.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{Name: "bare", URLs: []string{bareDir}}); err != nil {
		t.Fatal(err)
	}
	if err := work.Push(&gogit.PushOptions{
		RemoteName: "bare",
		RefSpecs:   []gitconfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestWeekStart(t *testing.T) {
	est := time.FixedZone("EST", -5*3600)
	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"monday midnight", time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC), time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC)},
		{"wednesday afternoon", time.Date(2024, 3, 6, 15, 30, 0, 0, time.UTC), time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC)},
		{"sunday belongs to the week that began the Monday before", time.Date(2024, 3, 10, 23, 59, 59, 0, time.UTC), time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC)},
		{"crosses month boundary", time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC), time.Date(2024, 2, 26, 0, 0, 0, 0, time.UTC)},
		{"crosses year boundary", time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"non-UTC input is bucketed by its UTC day", time.Date(2024, 3, 3, 20, 0, 0, 0, est), time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weekStart(tt.in)
			if !got.Equal(tt.want) || got.Location() != time.UTC {
				t.Errorf("weekStart(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestCodeService_GetCommitActivityAndFrequency(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	day := 24 * time.Hour
	svc := newInsightsRepo(t, "ada", "proj",
		insightsCommit{now.Add(-400 * day), "Old", "old@example.com", 5},
		insightsCommit{now.Add(-10 * day), "Ada", "ada@example.com", 8},
		insightsCommit{now.Add(-1 * day), "Ada", "ada@example.com", 4},
	)

	activity, err := svc.GetCommitActivity("ada", "proj")
	if err != nil {
		t.Fatalf("GetCommitActivity: %v", err)
	}
	if len(activity) != 52 {
		t.Fatalf("len = %d, want 52", len(activity))
	}
	if !activity[51].WeekStart.Equal(weekStart(now)) {
		t.Errorf("last bucket = %v, want current week %v", activity[51].WeekStart, weekStart(now))
	}
	total := 0
	for i, w := range activity {
		total += w.Total
		if i > 0 && !w.WeekStart.After(activity[i-1].WeekStart) {
			t.Errorf("buckets not strictly ascending at %d", i)
		}
	}
	if total != 2 {
		t.Errorf("total commits = %d, want 2 (the 400-day-old commit is outside the window)", total)
	}
	byWeek := map[time.Time]int{}
	for _, w := range activity {
		byWeek[w.WeekStart] = w.Total
	}
	if byWeek[weekStart(now.Add(-10*day))] != 1 || byWeek[weekStart(now.Add(-1*day))] != 1 {
		t.Errorf("per-week totals wrong: %v", byWeek)
	}

	freq, err := svc.GetCodeFrequency("ada", "proj")
	if err != nil {
		t.Fatalf("GetCodeFrequency: %v", err)
	}
	if len(freq) != 52 {
		t.Fatalf("len = %d, want 52", len(freq))
	}
	var add, del int
	byFreq := map[time.Time]CodeFrequencyWeek{}
	for _, w := range freq {
		add += w.Additions
		del += w.Deletions
		byFreq[w.WeekStart] = w
	}
	if add != 3 || del != 4 {
		t.Errorf("additions/deletions = %d/%d, want 3/4 (8 lines on top of the old commit's 5)", add, del)
	}
	if w := byFreq[weekStart(now.Add(-10*day))]; w.Additions != 3 || w.Deletions != 0 {
		t.Errorf("week of growing commit = %+v", w)
	}
	if w := byFreq[weekStart(now.Add(-1*day))]; w.Additions != 0 || w.Deletions != 4 {
		t.Errorf("week of shrinking commit = %+v", w)
	}
}

func TestCodeService_Insights_EmptyAndMissingRepo(t *testing.T) {
	t.Parallel()
	svc := newInsightsRepo(t, "ada", "empty")

	if _, err := svc.GetCommitActivity("ada", "empty"); !errors.Is(err, ErrEmptyRepo) {
		t.Errorf("GetCommitActivity empty: err = %v, want ErrEmptyRepo", err)
	}
	if _, err := svc.GetCodeFrequency("ada", "empty"); !errors.Is(err, ErrEmptyRepo) {
		t.Errorf("GetCodeFrequency empty: err = %v, want ErrEmptyRepo", err)
	}
	if _, err := svc.GetContributors("ada", "empty", "main"); err == nil {
		t.Error("GetContributors on an empty repo must fail to resolve the ref")
	}

	if _, err := svc.GetCommitActivity("ada", "missing"); !errors.Is(err, gogit.ErrRepositoryNotExists) {
		t.Errorf("GetCommitActivity missing: err = %v, want ErrRepositoryNotExists", err)
	}
	if _, err := svc.GetCodeFrequency("ada", "missing"); !errors.Is(err, gogit.ErrRepositoryNotExists) {
		t.Errorf("GetCodeFrequency missing: err = %v, want ErrRepositoryNotExists", err)
	}
	if _, err := svc.GetContributors("ada", "missing", "main"); !errors.Is(err, gogit.ErrRepositoryNotExists) {
		t.Errorf("GetContributors missing: err = %v, want ErrRepositoryNotExists", err)
	}
	if _, err := svc.GetCommitActivity("../escape", "x"); err == nil {
		t.Error("an unsafe owner must not open a repository")
	}
}

func TestCodeService_GetContributors_AggregatesByEmailAndSorts(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	h := time.Hour
	svc := newInsightsRepo(t, "ada", "proj",
		insightsCommit{now.Add(-5 * h), "Bob", "bob@example.com", 2},
		insightsCommit{now.Add(-4 * h), "Ada", "ada@example.com", 5},
		insightsCommit{now.Add(-3 * h), "Ada Renamed", "ada@example.com", 9},
		insightsCommit{now.Add(-2 * h), "Ada", "ada@example.com", 7},
	)

	got, err := svc.GetContributors("ada", "proj", "master")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("contributors = %+v, want 2 (same email is one contributor)", got)
	}
	ada, bob := got[0], got[1]
	if ada.Email != "ada@example.com" || ada.Commits != 3 || bob.Email != "bob@example.com" || bob.Commits != 1 {
		t.Fatalf("order/commits wrong: %+v", got)
	}
	if ada.Additions != 3+4 || ada.Deletions != 2 {
		t.Errorf("ada +%d -%d, want +7 -2", ada.Additions, ada.Deletions)
	}
	if bob.Additions != 2 || bob.Deletions != 0 {
		t.Errorf("bob +%d -%d, want +2 -0", bob.Additions, bob.Deletions)
	}

	if _, err := svc.GetContributors("ada", "proj", "no-such-branch"); err == nil {
		t.Error("unknown ref must be an error")
	}
}
