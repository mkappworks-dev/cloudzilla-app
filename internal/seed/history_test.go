package seed

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

var (
	testStart = time.Date(2025, 10, 1, 9, 0, 0, 0, time.UTC)
	testEnd   = time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)
)

func testSpec() historySpec {
	return historySpec{
		Module:      "example.test/ada/swift-ledger",
		Name:        "swift-ledger",
		Description: "A ledger.",
		Langs:       []*language{languages[0], languages[2]},
		Authors: []service.GitAuthor{
			{Name: "Ada Abara", Email: "ada@example.test"},
			{Name: "Bao Bianchi", Email: "bao@example.test"},
		},
		Start:       testStart,
		End:         testEnd,
		MainCommits: 12,
		Features: []featureSpec{
			{Branch: "feature/merged-one", Author: 1, Commits: 2, Merge: true},
			{Branch: "feature/merged-two", Author: 0, Commits: 3, Merge: true},
			{Branch: "feature/open-one", Author: 1, Commits: 2},
		},
		Tags: 2,
	}
}

func build(t *testing.T, spec historySpec, seed uint64) (*gogit.Repository, historyResult) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := buildHistory(dir, rand.New(rand.NewPCG(seed, seed)), spec)
	if err != nil {
		t.Fatalf("buildHistory: %v", err)
	}
	return repo, res
}

func commitsFrom(t *testing.T, repo *gogit.Repository, from plumbing.Hash) []*object.Commit {
	t.Helper()
	iter, err := repo.Log(&gogit.LogOptions{From: from})
	if err != nil {
		t.Fatal(err)
	}
	var out []*object.Commit
	if err := iter.ForEach(func(c *object.Commit) error { out = append(out, c); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuildHistory_BackdatesEveryCommitInsideTheWindow(t *testing.T) {
	spec := testSpec()
	repo, res := build(t, spec, 1)

	tips := []plumbing.Hash{res.MainTip}
	for _, f := range res.Features {
		tips = append(tips, f.Tip)
	}
	authors := map[string]bool{}
	for _, a := range spec.Authors {
		authors[a.Email] = true
	}
	for _, tip := range tips {
		for _, c := range commitsFrom(t, repo, tip) {
			if c.Author.When.Before(testStart) || c.Author.When.After(testEnd) {
				t.Errorf("commit %s dated %s, outside [%s, %s]", c.Hash, c.Author.When, testStart, testEnd)
			}
			if !authors[c.Author.Email] {
				t.Errorf("commit %s authored by %q, not one of the spec's authors", c.Hash, c.Author.Email)
			}
			for _, p := range c.ParentHashes {
				parent, err := repo.CommitObject(p)
				if err != nil {
					t.Fatal(err)
				}
				if parent.Author.When.After(c.Author.When) {
					t.Errorf("parent %s is newer than child %s", parent.Hash, c.Hash)
				}
			}
		}
	}
}

func TestBuildHistory_MainCarriesOneMergeCommitPerMergedFeature(t *testing.T) {
	repo, res := build(t, testSpec(), 2)

	ref, err := repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil || ref.Hash() != res.MainTip {
		t.Fatalf("refs/heads/main = %v (err %v), want %s", ref, err, res.MainTip)
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil || head.Target() != plumbing.NewBranchReferenceName("main") {
		t.Errorf("HEAD = %v (err %v), want a symbolic ref to main", head, err)
	}

	mergedParents := map[plumbing.Hash]bool{}
	for _, c := range commitsFrom(t, repo, res.MainTip) {
		if len(c.ParentHashes) == 2 {
			mergedParents[c.ParentHashes[1]] = true
		}
	}
	for _, f := range res.Features {
		if f.Merge != mergedParents[f.Tip] {
			t.Errorf("%s: merged into main = %v, want %v", f.Branch, mergedParents[f.Tip], f.Merge)
		}
		ref, err := repo.Reference(plumbing.NewBranchReferenceName(f.Branch), true)
		if err != nil || ref.Hash() != f.Tip {
			t.Errorf("%s ref = %v (err %v), want %s", f.Branch, ref, err, f.Tip)
		}
		if f.Title == "" {
			t.Errorf("%s has no title", f.Branch)
		}
	}
}

func TestBuildHistory_HunksPointAtLinesTheBranchAdded(t *testing.T) {
	repo, res := build(t, testSpec(), 3)

	for _, f := range res.Features {
		if len(f.Hunks) == 0 {
			t.Fatalf("%s has no hunks", f.Branch)
		}
		tip, err := repo.CommitObject(f.Tip)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range f.Hunks {
			file, err := tip.File(h.Path)
			if err != nil {
				t.Fatalf("%s: hunk file %s: %v", f.Branch, h.Path, err)
			}
			content, _ := file.Contents()
			lines := strings.Split(content, "\n")
			if h.Start < 1 || h.End > len(lines) || h.Start > h.End {
				t.Fatalf("%s: hunk %s:%d-%d outside a %d-line file", f.Branch, h.Path, h.Start, h.End, len(lines))
			}
			if got := lines[h.Start-1 : h.End]; strings.Join(got, "\n") != strings.Join(h.Lines, "\n") {
				t.Errorf("%s: hunk %s:%d-%d lines = %q, want %q", f.Branch, h.Path, h.Start, h.End, got, h.Lines)
			}
		}
	}
}

func TestBuildHistory_TagsAreAnnotatedAndBackdated(t *testing.T) {
	repo, res := build(t, testSpec(), 4)

	if len(res.Tags) != 2 {
		t.Fatalf("got %d tags, want 2", len(res.Tags))
	}
	for _, tr := range res.Tags {
		ref, err := repo.Reference(plumbing.NewTagReferenceName(tr.Name), true)
		if err != nil {
			t.Fatalf("tag %s: %v", tr.Name, err)
		}
		tag, err := repo.TagObject(ref.Hash())
		if err != nil {
			t.Fatalf("tag %s is not annotated: %v", tr.Name, err)
		}
		if tag.Tagger.When.Before(testStart) || tag.Tagger.When.After(testEnd) {
			t.Errorf("tag %s dated %s, outside the window", tr.Name, tag.Tagger.When)
		}
		if len(tr.Notes) == 0 {
			t.Errorf("tag %s has no release notes", tr.Name)
		}
	}
}

func TestBuildHistory_SameSeedBuildsTheSameHistory(t *testing.T) {
	_, a := build(t, testSpec(), 5)
	_, b := build(t, testSpec(), 5)
	if a.MainTip != b.MainTip {
		t.Errorf("main tips differ: %s vs %s", a.MainTip, b.MainTip)
	}
}
