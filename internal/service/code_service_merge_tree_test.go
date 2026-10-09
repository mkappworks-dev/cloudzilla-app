package service

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"
)

// newTipTestRepo's main and feature have diverged without conflicts: the case
// where a merge check builds the merged tree.
func TestPullReads_WriteNoObjects(t *testing.T) {
	tests := []struct {
		name string
		read func(r *tipTestRepo) error
	}{
		{"diff", func(r *tipTestRepo) error {
			_, err := r.svc.GetPullDiff(r.owner, r.name, "main", "feature")
			return err
		}},
		{"mergeability", func(r *tipTestRepo) error {
			_, err := r.svc.Mergeability(context.Background(), r.owner, r.name, "main", "feature")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			objects := filepath.Join(r.gitDir, "objects")
			before := countFiles(t, objects)
			if err := tt.read(r); err != nil {
				t.Fatal(err)
			}
			if written := countFiles(t, objects) - before; written != 0 {
				t.Errorf("wrote %d objects into the repo", written)
			}
		})
	}
}

var pullMerges = []struct {
	name  string
	merge func(t *testing.T, r *tipTestRepo) error
}{
	{"three-way", func(t *testing.T, r *tipTestRepo) error {
		return r.svc.ThreeWayMergePullRequest(r.owner, r.name, "main", "feature", branchTip(t, r.repo, "feature"), tipTestAuthor)
	}},
	{"squash", func(t *testing.T, r *tipTestRepo) error {
		return r.svc.SquashMergePullRequest(r.owner, r.name, "main", "feature", branchTip(t, r.repo, "feature"), tipTestAuthor)
	}},
}

func TestPullMerges_CombineBothSides(t *testing.T) {
	for _, tt := range pullMerges {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			if err := tt.merge(t, r); err != nil {
				t.Fatalf("merge: %v", err)
			}
			merged, err := r.repo.CommitObject(branchTip(t, r.repo, "main"))
			if err != nil {
				t.Fatalf("merged commit: %v", err)
			}
			tree, err := merged.Tree()
			if err != nil {
				t.Fatalf("merged tree: %v", err)
			}
			files := map[string]string{}
			if err := tree.Files().ForEach(func(f *gitobj.File) error {
				content, err := f.Contents()
				files[f.Name] = content
				return err
			}); err != nil {
				t.Fatalf("walk merged tree: %v", err)
			}
			want := map[string]string{"a.txt": "a\n", "f.txt": "one\ntwo\n", "m.txt": "m\n"}
			if !reflect.DeepEqual(files, want) {
				t.Errorf("merged files = %q, want %q", files, want)
			}
		})
	}
}

// Submodule commits live in the submodules' own repos, so tests needn't store them.
var (
	mainLib    = plumbing.NewHash("1111111111111111111111111111111111111111")
	featureLib = plumbing.NewHash("2222222222222222222222222222222222222222")
)

func TestPullMerges_KeepSymlinksAndSubmodules(t *testing.T) {
	for _, tt := range pullMerges {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			r.commitEntry(t, "main", "deps/main-lib", filemode.Submodule, mainLib)
			r.commitEntry(t, "feature", "deps/feature-lib", filemode.Submodule, featureLib)
			link := r.blob(t, "a.txt")
			r.commitEntry(t, "feature", "link", filemode.Symlink, link)
			if err := tt.merge(t, r); err != nil {
				t.Fatalf("merge: %v", err)
			}
			r.expectEntries(t, "main", map[string]mergeFile{
				"deps/main-lib":    {hash: mainLib, mode: filemode.Submodule},
				"deps/feature-lib": {hash: featureLib, mode: filemode.Submodule},
				"link":             {hash: link, mode: filemode.Symlink},
			})
		})
	}
}

func TestPullMerges_KeepModeChanges(t *testing.T) {
	for _, tt := range pullMerges {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			a := r.blob(t, "a\n")
			r.commitEntry(t, "feature", "a.txt", filemode.Executable, a)
			if err := tt.merge(t, r); err != nil {
				t.Fatalf("merge: %v", err)
			}
			r.expectEntries(t, "main", map[string]mergeFile{"a.txt": {hash: a, mode: filemode.Executable}})
		})
	}
}

func TestPullMerges_RefuseConflictingEdits(t *testing.T) {
	conflicts := []struct {
		name string
		// edit makes conflicting changes on main and feature, and returns main's tip.
		edit func(t *testing.T, r *tipTestRepo) plumbing.Hash
	}{
		{"both add a file", func(t *testing.T, r *tipTestRepo) plumbing.Hash {
			return r.commit(t, "main", "f.txt", "main's f\n")
		}},
		{"both add a submodule", func(t *testing.T, r *tipTestRepo) plumbing.Hash {
			r.commitEntry(t, "feature", "deps/lib", filemode.Submodule, featureLib)
			return r.commitEntry(t, "main", "deps/lib", filemode.Submodule, mainLib)
		}},
		{"chmod and edit", func(t *testing.T, r *tipTestRepo) plumbing.Hash {
			r.commit(t, "feature", "a.txt", "a\nb\n")
			return r.commitEntry(t, "main", "a.txt", filemode.Executable, r.blob(t, "a\n"))
		}},
		{"file against directory", func(t *testing.T, r *tipTestRepo) plumbing.Hash {
			r.commit(t, "feature", "lib/x.go", "x\n")
			return r.commit(t, "main", "lib", "lib\n")
		}},
		{"directory against file", func(t *testing.T, r *tipTestRepo) plumbing.Hash {
			r.commit(t, "feature", "lib", "lib\n")
			return r.commit(t, "main", "lib/x.go", "x\n")
		}},
		{"nested file against directory", func(t *testing.T, r *tipTestRepo) plumbing.Hash {
			r.commit(t, "feature", "src/lib/x.go", "x\n")
			return r.commit(t, "main", "src/lib", "lib\n")
		}},
		{"submodule against directory", func(t *testing.T, r *tipTestRepo) plumbing.Hash {
			r.commit(t, "feature", "deps/lib/x.go", "x\n")
			return r.commitEntry(t, "main", "deps/lib", filemode.Submodule, mainLib)
		}},
	}
	for _, c := range conflicts {
		for _, tt := range pullMerges {
			t.Run(c.name+"/"+tt.name, func(t *testing.T) {
				r := newTipTestRepo(t)
				tip := c.edit(t, r)
				if m, err := r.svc.Mergeability(context.Background(), r.owner, r.name, "main", "feature"); err != nil || !m.HasConflicts {
					t.Errorf("Mergeability = %+v, %v; want conflicts", m, err)
				}
				if err := tt.merge(t, r); err == nil || !strings.Contains(err.Error(), "conflicting changes") {
					t.Errorf("err = %v, want a conflict error", err)
				}
				if got := branchTip(t, r.repo, "main"); got != tip {
					t.Errorf("main = %s, want it left at %s", got, tip)
				}
			})
		}
	}
}

func TestApplySuggestion_KeepsSubmodules(t *testing.T) {
	r := newTipTestRepo(t)
	r.commitEntry(t, "feature", "deps/feature-lib", filemode.Submodule, featureLib)
	if _, err := r.svc.ApplySuggestion(r.owner, r.name, "feature", "f.txt", 1, "uno", tipTestAuthor); err != nil {
		t.Fatalf("ApplySuggestion: %v", err)
	}
	r.expectEntries(t, "feature", map[string]mergeFile{
		"f.txt":            {hash: r.blob(t, "uno\ntwo\n"), mode: filemode.Regular},
		"deps/feature-lib": {hash: featureLib, mode: filemode.Submodule},
	})
}

// commitEntry commits hash at path on branch with any mode, where CommitFile
// writes only regular files, and returns the new tip.
func (r *tipTestRepo) commitEntry(t *testing.T, branch, path string, mode filemode.FileMode, hash plumbing.Hash) plumbing.Hash {
	t.Helper()
	parent := branchTip(t, r.repo, branch)
	sig := tipTestAuthor.signature(time.Now())
	tip := r.put(t, (&gitobj.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      "Set " + path,
		TreeHash:     r.withEntry(t, r.tipTree(t, branch), path, gitobj.TreeEntry{Mode: mode, Hash: hash}),
		ParentHashes: []plumbing.Hash{parent},
	}).Encode)
	setBranch(t, r.repo, branch, tip)
	return tip
}

// withEntry stores tree with e at path, adding directories as needed, and
// returns the new tree's hash.
func (r *tipTestRepo) withEntry(t *testing.T, tree *gitobj.Tree, path string, e gitobj.TreeEntry) plumbing.Hash {
	t.Helper()
	name, rest, nested := strings.Cut(path, "/")
	sub := &gitobj.Tree{}
	var entries []gitobj.TreeEntry
	for _, old := range tree.Entries {
		if old.Name != name {
			entries = append(entries, old)
			continue
		}
		if nested {
			var err error
			if sub, err = tree.Tree(name); err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
		}
	}
	if nested {
		e = gitobj.TreeEntry{Mode: filemode.Dir, Hash: r.withEntry(t, sub, rest, e)}
	}
	e.Name = name
	entries = append(entries, e)
	sort.Sort(gitobj.TreeEntrySorter(entries))
	return r.put(t, (&gitobj.Tree{Entries: entries}).Encode)
}

// expectEntries checks branch's tip tree for want's entries, keyed by path.
func (r *tipTestRepo) expectEntries(t *testing.T, branch string, want map[string]mergeFile) {
	t.Helper()
	tree := r.tipTree(t, branch)
	for path, w := range want {
		e, err := tree.FindEntry(path)
		if err != nil {
			t.Errorf("%s %s: %v", branch, path, err)
			continue
		}
		if got := (mergeFile{hash: e.Hash, mode: e.Mode}); got != w {
			t.Errorf("%s %s = %s %s, want %s %s", branch, path, got.mode, got.hash, w.mode, w.hash)
		}
	}
}

func (r *tipTestRepo) tipTree(t *testing.T, branch string) *gitobj.Tree {
	t.Helper()
	tip, err := r.repo.CommitObject(branchTip(t, r.repo, branch))
	if err != nil {
		t.Fatalf("%s tip: %v", branch, err)
	}
	tree, err := tip.Tree()
	if err != nil {
		t.Fatalf("%s tree: %v", branch, err)
	}
	return tree
}

func (r *tipTestRepo) blob(t *testing.T, content string) plumbing.Hash {
	t.Helper()
	h, err := writeBlob(r.repo, []byte(content))
	if err != nil {
		t.Fatalf("write blob: %v", err)
	}
	return h
}

// put stores the object encode writes and returns its hash.
func (r *tipTestRepo) put(t *testing.T, encode func(plumbing.EncodedObject) error) plumbing.Hash {
	t.Helper()
	obj := r.repo.Storer.NewEncodedObject()
	if err := encode(obj); err != nil {
		t.Fatalf("encode object: %v", err)
	}
	h, err := r.repo.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatalf("store object: %v", err)
	}
	return h
}
