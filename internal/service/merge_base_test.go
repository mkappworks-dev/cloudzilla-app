package service

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func commitOf(t *testing.T, repo *gogit.Repository, h plumbing.Hash) *object.Commit {
	t.Helper()
	c, err := repo.CommitObject(h)
	if err != nil {
		t.Fatalf("read commit %s: %v", h, err)
	}
	return c
}

// isAncestor's walk never stops on a date, so it must match reachability on
// any history, commits dated before their parents included.
func TestIsAncestor_MatchesReachability(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for range 200 {
		commits := randomHistory(rng, true)
		repo, hashes := writeRangeHistory(t, commits)
		reaches := reachability(commits)
		for range 20 {
			a, b := commits[rng.IntN(len(commits))].name, commits[rng.IntN(len(commits))].name
			got, err := isAncestor(repo, commitOf(t, repo, hashes[a]), commitOf(t, repo, hashes[b]))
			if err != nil {
				t.Fatalf("isAncestor(%s, %s): %v", a, b, err)
			}
			if want := reaches(b)[a]; got != want {
				t.Fatalf("isAncestor(%s, %s) = %v, want %v\nhistory: %v", a, b, got, want, commits)
			}
		}
	}
}

// mergeBases must return exactly the common ancestors that no other one
// reaches, on any history.
func TestMergeBases_MatchesReachability(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	for range 200 {
		commits := randomHistory(rng, true)
		repo, hashes := writeRangeHistory(t, commits)
		reaches := reachability(commits)
		for range 20 {
			a, b := commits[rng.IntN(len(commits))].name, commits[rng.IntN(len(commits))].name
			fromA, fromB := reaches(a), reaches(b)
			var want []string
			for c := range fromA {
				if !fromB[c] {
					continue
				}
				best := true
				for d := range fromA {
					if d != c && fromB[d] && reaches(d)[c] {
						best = false
						break
					}
				}
				if best {
					want = append(want, c)
				}
			}
			bases, err := mergeBases(repo, commitOf(t, repo, hashes[a]), commitOf(t, repo, hashes[b]))
			if err != nil {
				t.Fatalf("mergeBases(%s, %s): %v", a, b, err)
			}
			var got []string
			for _, c := range bases {
				got = append(got, c.Message)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("mergeBases(%s, %s) = %v, want %v\nhistory: %v", a, b, got, want, commits)
			}
		}
	}
}

// findMergeBase must pick what go-git's Commit.MergeBase picked before it, on
// any history, including which of several criss-cross merge bases.
func TestFindMergeBase_MatchesGoGit(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	for range 200 {
		commits := randomHistory(rng, true)
		repo, hashes := writeRangeHistory(t, commits)
		names := map[plumbing.Hash]string{}
		for name, h := range hashes {
			names[h] = name
		}
		nameOf := func(c *object.Commit) string {
			if c == nil {
				return "none"
			}
			return names[c.Hash]
		}
		for range 20 {
			a := commitOf(t, repo, hashes[commits[rng.IntN(len(commits))].name])
			b := commitOf(t, repo, hashes[commits[rng.IntN(len(commits))].name])
			bases, err := a.MergeBase(b)
			if err != nil {
				t.Fatalf("go-git MergeBase(%s, %s): %v", nameOf(a), nameOf(b), err)
			}
			want, wantErr := (*object.Commit)(nil), ErrNoCommonAncestor
			if len(bases) > 0 {
				want, wantErr = bases[0], nil
			}
			got, err := findMergeBase(repo, a, b)
			if !errors.Is(err, wantErr) || nameOf(got) != nameOf(want) {
				t.Fatalf("findMergeBase(%s, %s) = %s, %v; want %s, %v\nhistory: %v", nameOf(a), nameOf(b), nameOf(got), err, nameOf(want), wantErr, commits)
			}
		}
	}
}

// The push shapes sit on history whose commits are missing 20 below the fork,
// so a walk through all of either tip's history fails instead of answering.
func TestIsAncestor_ReadsOnlyTheHistorySinceTheFork(t *testing.T) {
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
				p := shape.build(h, h.shallowLine(20))

				got, err := isAncestor(h.repo, commitOf(t, h.repo, p.old), commitOf(t, h.repo, p.new))
				if err != nil || got == p.forced {
					t.Errorf("isAncestor(old, new) = %v, %v; want %v, nil", got, err, !p.forced)
				}
			})
		}
	}
}

// A force push that drops a merge reads none of the merged branch, whose
// history is missing below its last five commits.
func TestIsAncestor_ForcePushDroppingAMerge(t *testing.T) {
	tests := []struct {
		name string
		new  func(h *pushHistory, before plumbing.Hash) plumbing.Hash
	}{
		{"back to before the merge", func(h *pushHistory, before plumbing.Hash) plumbing.Hash {
			return before
		}},
		{"onto the merge's first parent", func(h *pushHistory, before plumbing.Hash) plumbing.Hash {
			return h.commit("Instead of the merge", before)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newPushHistory(t, t.TempDir(), "tester@example.com", time.Minute)
			merged := h.shallowLine(5)
			before := h.line(3, h.shallowLine(20))
			old := h.commit("Merge branch", before, merged)
			newTip := tt.new(h, before)

			got, err := isAncestor(h.repo, commitOf(t, h.repo, old), commitOf(t, h.repo, newTip))
			if err != nil || got {
				t.Errorf("isAncestor(old, new) = %v, %v; want false, nil", got, err)
			}
		})
	}
}

func TestFindMergeBase_ReadsOnlyTheHistorySinceTheFork(t *testing.T) {
	tests := []struct {
		name string
		// build returns the PR's base and head tips and their merge base.
		build func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash)
	}{
		{"base moved on", func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash) {
			head = h.commit("Feature 2", h.commit("Feature 1", fork))
			base = h.commit("Main change", fork)
			return base, head, fork
		}},
		{"head merged base, then base moved on", func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash) {
			feature := h.commit("Feature 1", fork)
			merged := h.commit("Main change 1", fork)
			head = h.commit("Merge main", feature, merged)
			base = h.commit("Main change 2", merged)
			return base, head, merged
		}},
		{"head is ahead of base", func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash) {
			return fork, h.commit("Feature 1", fork), fork
		}},
		// The merged branch's history is missing below its last five commits.
		{"head is ahead of base and merged another branch", func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash) {
			merged := h.shallowLine(5)
			return fork, h.commit("Merge branch", h.commit("Feature 1", fork), merged), fork
		}},
		{"head was merged into base", func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash) {
			head = h.commit("Feature 1", fork)
			base = h.commit("Merge feature", h.commit("Main change", fork), head)
			return base, head, head
		}},
		{"head was merged into base with another branch", func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash) {
			merged := h.shallowLine(5)
			head = h.commit("Feature 1", fork)
			return h.commit("Merge feature and branch", head, merged), head, head
		}},
		// Both sides merged each other's first commit: those two are the
		// merge bases, and the newer one is picked.
		{"criss-cross merges", func(h *pushHistory, fork plumbing.Hash) (base, head, want plumbing.Hash) {
			older := h.commit("Main change", fork)
			newer := h.commit("Feature 1", fork)
			base = h.commit("Main change 2", h.commit("Merge feature", older, newer))
			head = h.commit("Feature 2", h.commit("Merge main", newer, older))
			return base, head, newer
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newPushHistory(t, t.TempDir(), "tester@example.com", time.Minute)
			base, head, want := tt.build(h, h.shallowLine(20))

			got, err := findMergeBase(h.repo, commitOf(t, h.repo, base), commitOf(t, h.repo, head))
			if err != nil {
				t.Fatalf("findMergeBase: %v", err)
			}
			if got.Hash != want {
				t.Errorf("findMergeBase = %s, want %s", got.Hash, want)
			}
		})
	}
}
