package service

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var pushedBranch = plumbing.NewBranchReferenceName("main")

// pushHistory writes commits straight into a bare repo, each dated step after
// the one before. A zero step dates them all in the same second, as scripted
// commits and quick rebases are.
type pushHistory struct {
	t     *testing.T
	repo  *gogit.Repository
	email string
	when  time.Time
	step  time.Duration
}

func newPushHistory(t *testing.T, gitDir, email string, step time.Duration) *pushHistory {
	t.Helper()
	repo, err := gogit.PlainInit(gitDir, true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	return &pushHistory{t: t, repo: repo, email: email, when: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), step: step}
}

func (h *pushHistory) commit(msg string, parents ...plumbing.Hash) plumbing.Hash {
	h.t.Helper()
	h.when = h.when.Add(h.step)
	return h.commitAt(h.when, msg, parents...)
}

func (h *pushHistory) commitAt(when time.Time, msg string, parents ...plumbing.Hash) plumbing.Hash {
	h.t.Helper()
	sig := object.Signature{Name: "Tester", Email: h.email, When: when}
	return testutil.WriteCommitBy(h.t, h.repo.Storer, sig, msg, parents...)
}

// line writes n commits in a row on top of parents and returns the last.
func (h *pushHistory) line(n int, parents ...plumbing.Hash) plumbing.Hash {
	h.t.Helper()
	for i := 1; i <= n; i++ {
		parents = []plumbing.Hash{h.commit(fmt.Sprintf("Commit %d", i), parents...)}
	}
	return parents[0]
}

// shallowLine is line(n) on a parent missing from the repo, as past a shallow
// clone's boundary, so a walk through all of its history fails.
func (h *pushHistory) shallowLine(n int) plumbing.Hash {
	h.t.Helper()
	return h.line(n, plumbing.NewHash("0123456789abcdef0123456789abcdef01234567"))
}

// testPush moves main from old to new, adding the commits in added, newest
// first. A forced push drops commits main had.
type testPush struct {
	old, new plumbing.Hash
	added    []plumbing.Hash
	forced   bool
}

func (p testPush) commands() []*packp.Command {
	return []*packp.Command{{Name: pushedBranch, Old: p.old, New: p.new}}
}

var pushShapes = []struct {
	name  string
	build func(h *pushHistory, fork plumbing.Hash) testPush
}{
	{"fast-forward", func(h *pushHistory, fork plumbing.Hash) testPush {
		first := h.commit("Pushed 1", fork)
		second := h.commit("Pushed 2", first)
		return testPush{old: fork, new: second, added: []plumbing.Hash{second, first}}
	}},
	// What `git pull` pushes once main has moved on.
	{"old tip as second parent", func(h *pushHistory, fork plumbing.Hash) testPush {
		local := h.commit("Local change", fork)
		old := h.commit("Upstream change", fork)
		merge := h.commit("Merge origin/main", local, old)
		return testPush{old: old, new: merge, added: []plumbing.Hash{merge, local}}
	}},
	// What merging a feature branch into main pushes.
	{"old tip as first parent", func(h *pushHistory, fork plumbing.Hash) testPush {
		part1 := h.commit("Feature part 1", fork)
		part2 := h.commit("Feature part 2", part1)
		old := h.commit("Main change", fork)
		merge := h.commit("Merge feature", old, part2)
		return testPush{old: old, new: merge, added: []plumbing.Hash{merge, part2, part1}}
	}},
	{"force-push of an amended tip", func(h *pushHistory, fork plumbing.Hash) testPush {
		old := h.commit("Draft", fork)
		amended := h.commit("Amended draft", fork)
		return testPush{old: old, new: amended, added: []plumbing.Hash{amended}, forced: true}
	}},
	{"force-push back to an ancestor", func(h *pushHistory, fork plumbing.Hash) testPush {
		old := h.commit("Dropped 2", h.commit("Dropped 1", fork))
		return testPush{old: old, new: fork, forced: true}
	}},
	{"force-push of an unrelated history", func(h *pushHistory, fork plumbing.Hash) testPush {
		root := h.commit("Unrelated root")
		tip := h.commit("Unrelated tip", root)
		return testPush{old: fork, new: tip, added: []plumbing.Hash{tip, root}, forced: true}
	}},
}

func assertPushSummary(t *testing.T, got []model.PushSummary, added []plumbing.Hash) {
	t.Helper()
	if len(added) == 0 {
		if len(got) != 0 {
			t.Errorf("summaries = %+v, want none", got)
		}
		return
	}
	if len(got) != 1 {
		t.Fatalf("summaries = %+v, want one for main", got)
	}
	if got[0].CommitTotal != len(added) {
		t.Errorf("CommitTotal = %d, want %d", got[0].CommitTotal, len(added))
	}
	var listed, want []string
	for _, c := range got[0].Commits {
		listed = append(listed, c.SHA)
	}
	for _, h := range added[:min(len(added), pushSummaryCommitCap)] {
		want = append(want, h.String()[:7])
	}
	if !reflect.DeepEqual(listed, want) {
		t.Errorf("listed %v, want %v", listed, want)
	}
}

func TestPushSummaries_UpdatedBranch(t *testing.T) {
	dates := []struct {
		name string
		step time.Duration
	}{
		{"a minute apart", time.Minute},
		{"in one second", 0},
	}
	for _, shape := range pushShapes {
		for _, d := range dates {
			t.Run(shape.name+", commits "+d.name, func(t *testing.T) {
				h := newPushHistory(t, t.TempDir(), "tester@example.com", d.step)
				p := shape.build(h, h.line(3))

				assertPushSummary(t, (&RepoService{}).PushSummaries(h.repo, p.commands()), p.added)
			})
		}
	}
}

// A walk through all of the old tip's history fails instead of summarizing
// the push.
func TestPushSummaries_ReadsOnlyTheHistorySinceTheFork(t *testing.T) {
	for _, shape := range pushShapes {
		t.Run(shape.name, func(t *testing.T) {
			h := newPushHistory(t, t.TempDir(), "tester@example.com", time.Minute)
			p := shape.build(h, h.shallowLine(20))

			assertPushSummary(t, (&RepoService{}).PushSummaries(h.repo, p.commands()), p.added)
		})
	}
}

// A commit from a machine whose clock is behind sorts after older history, so
// the walk keeps the fork point and its ancestors before old's side reaches them.
func TestPushSummaries_OldTipDatedBeforeItsParent(t *testing.T) {
	h := newPushHistory(t, t.TempDir(), "tester@example.com", time.Minute)
	fork := h.line(3)
	old := h.commitAt(h.when.Add(-24*time.Hour), "Draft", fork)
	amended := h.commit("Amended draft", fork)

	got := (&RepoService{}).PushSummaries(h.repo, testPush{old: old, new: amended}.commands())

	assertPushSummary(t, got, []plumbing.Hash{amended})
}

func TestPushSummaries_NewBranchStopsAtTheWalkCap(t *testing.T) {
	h := newPushHistory(t, t.TempDir(), "tester@example.com", time.Minute)
	tip := h.commit("Root")
	history := []plumbing.Hash{tip}
	for i := 1; i < pushSummaryWalkCap+10; i++ {
		tip = h.commit(fmt.Sprintf("Commit %d", i), tip)
		history = append([]plumbing.Hash{tip}, history...)
	}

	got := (&RepoService{}).PushSummaries(h.repo, []*packp.Command{{Name: pushedBranch, New: tip}})

	assertPushSummary(t, got, history[:pushSummaryWalkCap])
}

// The cap bounds a new branch's walk; an update's walk ends at the old tip.
func TestPushSummaries_UpdatedBranchCountsPastTheWalkCap(t *testing.T) {
	h := newPushHistory(t, t.TempDir(), "tester@example.com", time.Minute)
	old := h.line(3)
	tip := old
	var added []plumbing.Hash
	for i := range pushSummaryWalkCap + 10 {
		tip = h.commit(fmt.Sprintf("Pushed %d", i), tip)
		added = append([]plumbing.Hash{tip}, added...)
	}

	got := (&RepoService{}).PushSummaries(h.repo, testPush{old: old, new: tip}.commands())

	assertPushSummary(t, got, added)
}

func TestRepoService_OnPostReceive_IngestsOnlyThePushedCommits(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	root := t.TempDir()
	users := store.NewUserStore(db)
	repoSvc := NewRepoService(store.NewRepoStore(db), users, store.NewOrgStore(db),
		NewContributorStatsService(store.NewContributorStatsStore(db), users),
		NewCodeService(config.GitConfig{ReposRoot: root}), config.GitConfig{ReposRoot: root})

	for i, shape := range pushShapes {
		t.Run(shape.name, func(t *testing.T) {
			repo, err := repoSvc.GetByID(ctx, testutil.SeedRepo(t, db, ownerID, owner, fmt.Sprintf("%s_%d", suffix, i)))
			if err != nil {
				t.Fatalf("get repo: %v", err)
			}
			h := newPushHistory(t, filepath.Join(root, owner, repo.Name+".git"), owner+"@test.invalid", time.Minute)
			p := shape.build(h, h.line(3))

			if err := repoSvc.OnPostReceive(ctx, repo, h.repo, p.commands()); err != nil {
				t.Fatalf("OnPostReceive: %v", err)
			}

			rows, err := db.QueryContext(ctx, `SELECT sha FROM commits_ingested WHERE repo_id = $1 ORDER BY sha`, repo.ID)
			if err != nil {
				t.Fatalf("list ingested commits: %v", err)
			}
			defer rows.Close()
			var got []string
			for rows.Next() {
				var sha string
				if err := rows.Scan(&sha); err != nil {
					t.Fatalf("scan sha: %v", err)
				}
				got = append(got, sha)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("list ingested commits: %v", err)
			}
			var want []string
			for _, h := range p.added {
				want = append(want, h.String())
			}
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ingested %v, want %v", got, want)
			}
		})
	}
}
