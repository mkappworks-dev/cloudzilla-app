package gittransport_test

import (
	"slices"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

func sortedHashes[T interface{ String() string }](hs []T) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.String())
	}
	slices.Sort(out)
	return out
}

func commitHashes(cs []*object.Commit) []plumbing.Hash {
	out := make([]plumbing.Hash, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Hash)
	}
	return out
}

func TestAddedCommits(t *testing.T) {
	r := newPushRepo(t)
	repo := objects{t, r.repo.Storer}
	tree := repo.tree()
	a := repo.commit("a", tree, r.base)
	b := repo.commit("b", tree, a)
	side := repo.commit("side", tree, r.base)
	shared := repo.commit("shared", tree, b)
	tip1 := repo.commit("tip1", tree, shared)
	tip2 := repo.commit("tip2", tree, shared)
	tagged := repo.tag(tip2, plumbing.CommitObject)

	for _, tc := range []struct {
		name string
		tips []plumbing.Hash
		want []plumbing.Hash
	}{
		{"new branch counts only what no ref reaches", []plumbing.Hash{b}, []plumbing.Hash{a, b}},
		{"a tip a ref already holds adds nothing", []plumbing.Hash{r.base}, nil},
		{"two tips share their commits once", []plumbing.Hash{tip1, tip2}, []plumbing.Hash{a, b, shared, tip1, tip2}},
		{"an annotated tag is read through to its commit", []plumbing.Hash{tagged}, []plumbing.Hash{a, b, shared, tip2}},
		{"an unrelated branch off main", []plumbing.Hash{side}, []plumbing.Hash{side}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gittransport.AddedCommits(r.repo.Storer, tc.tips)
			if err != nil {
				t.Fatalf("AddedCommits: %v", err)
			}
			if !slices.Equal(sortedHashes(commitHashes(got)), sortedHashes(tc.want)) {
				t.Errorf("got %v, want %v", sortedHashes(commitHashes(got)), sortedHashes(tc.want))
			}
		})
	}
}
