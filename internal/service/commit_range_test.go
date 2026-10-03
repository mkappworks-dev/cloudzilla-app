package service

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// rangeCommit is a commit in a test history, dated at after a fixed epoch.
type rangeCommit struct {
	name    string
	parents []string
	at      time.Duration
}

func writeRangeHistory(t *testing.T, commits []rangeCommit) (*gogit.Repository, map[string]plumbing.Hash) {
	t.Helper()
	repo, err := gogit.Init(memory.NewStorage(), nil)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	epoch := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	hashes := map[string]plumbing.Hash{}
	for _, c := range commits {
		var parents []plumbing.Hash
		for _, p := range c.parents {
			parents = append(parents, hashes[p])
		}
		sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: epoch.Add(c.at)}
		hashes[c.name] = testutil.WriteCommitBy(t, repo.Storer, sig, c.name, parents...)
	}
	return repo, hashes
}

func rangeNames(t *testing.T, repo *gogit.Repository, hashes map[string]plumbing.Hash, old, tip string) []string {
	t.Helper()
	commits, err := commitRange(repo, hashes[old], hashes[tip])
	if err != nil {
		t.Fatalf("commitRange(%s, %s): %v", old, tip, err)
	}
	var names []string
	for _, c := range commits {
		names = append(names, c.Message)
	}
	return names
}

// With no commit dated before its parent, commitRange must list exactly the
// commits tip reaches and old doesn't. Random histories, from all distinct
// dates to nearly all in one second, with merges, extra roots and rewinds,
// exercise the stop rule that hand-built cases can't pin.
func TestCommitRange_MatchesReachability(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		sameSecond := rng.Float64()
		var commits []rangeCommit
		parentsOf := map[string][]string{}
		var at time.Duration
		for i := range 25 {
			var parents []string
			if i > 0 && rng.IntN(10) > 0 {
				for _, j := range rng.Perm(i)[:1+rng.IntN(min(i, 3))] {
					parents = append(parents, fmt.Sprintf("c%d", j))
				}
			}
			if rng.Float64() >= sameSecond {
				at += time.Duration(1+rng.IntN(60)) * time.Second
			}
			name := fmt.Sprintf("c%d", i)
			commits = append(commits, rangeCommit{name, parents, at})
			parentsOf[name] = parents
		}
		repo, hashes := writeRangeHistory(t, commits)

		reaches := func(from string) map[string]bool {
			seen := map[string]bool{}
			stack := []string{from}
			for len(stack) > 0 {
				c := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if !seen[c] {
					seen[c] = true
					stack = append(stack, parentsOf[c]...)
				}
			}
			return seen
		}
		for range 20 {
			old, tip := commits[rng.IntN(len(commits))].name, commits[rng.IntN(len(commits))].name
			fromOld := reaches(old)
			var want []string
			for c := range reaches(tip) {
				if !fromOld[c] {
					want = append(want, c)
				}
			}
			got := rangeNames(t, repo, hashes, old, tip)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("commitRange(%s, %s) = %v, want %v\nhistory: %v", old, tip, got, want, commits)
			}
		}
	}
}

func TestCommitRange_ClockSkew(t *testing.T) {
	// O reaches P through N, which a slow clock dated a day before P. N is
	// the last commit the walk would pop, after it has settled.
	behind := []rangeCommit{{"L0", nil, time.Minute}}
	for i := 1; i <= 6; i++ {
		behind = append(behind, rangeCommit{fmt.Sprintf("L%d", i), []string{fmt.Sprintf("L%d", i-1)}, time.Duration(i+1) * time.Minute})
	}
	behind = append(behind,
		rangeCommit{"B", []string{"L6"}, 50 * time.Minute},
		rangeCommit{"P", []string{"B"}, 100 * time.Minute},
		rangeCommit{"N", []string{"P"}, -24 * time.Hour},
		rangeCommit{"X", []string{"B"}, 60 * time.Minute},
		rangeCommit{"O", []string{"N", "X"}, 200 * time.Minute},
		rangeCommit{"Q", []string{"P"}, 150 * time.Minute},
		rangeCommit{"T", []string{"Q", "O"}, 300 * time.Minute},
	)
	tests := []struct {
		name     string
		commits  []rangeCommit
		old, tip string
		want     []string
	}{
		{"old reaches a kept commit through a commit dated before it", behind, "O", "T", []string{"T", "Q"}},
		// The walk looks settled once it pops c0; only the slop goes on to c4,
		// which reaches c1.
		{"slop reaches a commit dated before its parents", []rangeCommit{
			{"c0", nil, 0},
			{"c1", []string{"c0"}, 2 * time.Second},
			{"c2", []string{"c1"}, 5 * time.Second},
			{"c4", []string{"c2", "c0"}, -15 * time.Second},
		}, "c4", "c1", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, hashes := writeRangeHistory(t, tt.commits)
			if got := rangeNames(t, repo, hashes, tt.old, tt.tip); !slices.Equal(got, tt.want) {
				t.Errorf("commitRange(%s, %s) = %v, want %v", tt.old, tt.tip, got, tt.want)
			}
		})
	}
}
