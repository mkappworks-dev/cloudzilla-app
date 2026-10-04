package service

import (
	"errors"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestSplitRefPath(t *testing.T) {
	r := newTipTestRepo(t)
	if err := r.svc.CreateBranch(r.owner, r.name, "fix/render-cache", "main"); err != nil {
		t.Fatalf("create fix/render-cache: %v", err)
	}
	if err := r.svc.CreateTag(r.owner, r.name, "v1/rc", "main"); err != nil {
		t.Fatalf("create v1/rc: %v", err)
	}

	tests := []struct{ in, ref, path string }{
		{"main", "main", ""},
		{"main/lib/a.go", "main", "lib/a.go"},
		{"fix/render-cache", "fix/render-cache", ""},
		{"fix/render-cache/", "fix/render-cache", ""},
		{"fix/render-cache/lib/a.go", "fix/render-cache", "lib/a.go"},
		{"v1/rc/a.go", "v1/rc", "a.go"},
		// A prefix only matches at a segment boundary.
		{"fix/render-cache-old/a.go", "fix", "render-cache-old/a.go"},
		// SHAs and unknown refs keep the first segment, as before.
		{r.mainTip.String() + "/a.txt", r.mainTip.String(), "a.txt"},
		{"nope/a.go", "nope", "a.go"},
	}
	check := func(t *testing.T) {
		for _, tt := range tests {
			ref, path := r.svc.SplitRefPath(r.owner, r.name, tt.in)
			if ref != tt.ref || path != tt.path {
				t.Errorf("SplitRefPath(%q) = %q, %q; want %q, %q", tt.in, ref, path, tt.ref, tt.path)
			}
		}
	}
	t.Run("loose refs", check)
	t.Run("packed refs", func(t *testing.T) {
		if err := r.repo.Storer.(interface{ PackRefs() error }).PackRefs(); err != nil {
			t.Fatalf("pack refs: %v", err)
		}
		check(t)
	})
}

// newTagTestRepo is a tipTestRepo whose main tip carries a lightweight tag, an
// annotated tag and an annotated tag of that annotated tag.
func newTagTestRepo(t *testing.T) *tipTestRepo {
	t.Helper()
	r := newTipTestRepo(t)
	// PlainInit points HEAD at master, and an unborn HEAD turns every miss into ErrEmptyRepo.
	if err := r.repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatalf("point HEAD at main: %v", err)
	}
	if err := r.svc.CreateTag(r.owner, r.name, "v1-light", "main"); err != nil {
		t.Fatalf("create lightweight tag: %v", err)
	}
	annotated := annotateTag(t, r.repo, "v1-annotated", r.mainTip)
	annotateTag(t, r.repo, "v1-nested", annotated)
	return r
}

func annotateTag(t *testing.T, repo *gogit.Repository, name string, target plumbing.Hash) plumbing.Hash {
	t.Helper()
	ref, err := repo.CreateTag(name, target, &gogit.CreateTagOptions{
		Message: "Release " + name,
		Tagger:  &object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(1700000000, 0)},
	})
	if err != nil {
		t.Fatalf("create annotated tag %s: %v", name, err)
	}
	return ref.Hash()
}

func TestTagRefs_ResolveToTaggedCommit(t *testing.T) {
	r := newTagTestRepo(t)
	for _, tag := range []string{"v1-light", "v1-annotated", "v1-nested"} {
		t.Run(tag, func(t *testing.T) {
			commit, displayRef, err := r.svc.ResolveRef(r.owner, r.name, tag)
			if err != nil {
				t.Fatalf("ResolveRef: %v", err)
			}
			if commit.Hash != r.mainTip || displayRef != tag {
				t.Errorf("ResolveRef = %s, %q; want %s, %q", commit.Hash, displayRef, r.mainTip, tag)
			}

			tree, err := r.svc.GetTree(r.owner, r.name, tag, "")
			if err != nil {
				t.Fatalf("GetTree: %v", err)
			}
			if len(tree.Entries) != 2 {
				t.Errorf("GetTree entries = %v, want a.txt and m.txt", tree.Entries)
			}

			blob, err := r.svc.GetBlob(r.owner, r.name, tag, "m.txt")
			if err != nil {
				t.Fatalf("GetBlob: %v", err)
			}
			if len(blob.Lines) == 0 || blob.Lines[0].Text != "m" {
				t.Errorf("GetBlob lines = %v, want m first", blob.Lines)
			}
		})
	}
}

func TestTagRefs_TagOfNonCommitIsNotFound(t *testing.T) {
	r := newTagTestRepo(t)
	commit, err := r.repo.CommitObject(r.mainTip)
	if err != nil {
		t.Fatalf("load main tip: %v", err)
	}
	file, err := commit.File("a.txt")
	if err != nil {
		t.Fatalf("load a.txt: %v", err)
	}
	annotateTag(t, r.repo, "tree-tag", commit.TreeHash)
	annotateTag(t, r.repo, "nested-blob-tag", annotateTag(t, r.repo, "blob-tag", file.Hash))

	for _, tag := range []string{"tree-tag", "blob-tag", "nested-blob-tag"} {
		t.Run(tag, func(t *testing.T) {
			if _, _, err := r.svc.ResolveRef(r.owner, r.name, tag); !errors.Is(err, ErrRefNotFound) {
				t.Errorf("ResolveRef err = %v, want ErrRefNotFound", err)
			}
		})
	}
}

func TestListRefsPeeled_TagHashIsTaggedCommit(t *testing.T) {
	r := newTagTestRepo(t)
	refs, err := r.svc.ListRefsPeeled(r.owner, r.name, "main")
	if err != nil {
		t.Fatalf("ListRefsPeeled: %v", err)
	}
	if len(refs.Tags) != 3 {
		t.Fatalf("tags = %v, want 3", refs.Tags)
	}
	want := r.mainTip.String()[:7]
	for _, tag := range refs.Tags {
		if tag.Hash != want {
			t.Errorf("tag %s hash = %s, want tagged commit %s", tag.Name, tag.Hash, want)
		}
	}
}
