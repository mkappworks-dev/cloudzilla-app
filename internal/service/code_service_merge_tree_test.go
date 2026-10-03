package service

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	merge func(r *tipTestRepo) error
}{
	{"three-way", func(r *tipTestRepo) error {
		return r.svc.ThreeWayMergePullRequest(r.owner, r.name, "main", "feature", tipTestAuthor)
	}},
	{"squash", func(r *tipTestRepo) error {
		return r.svc.SquashMergePullRequest(r.owner, r.name, "main", "feature", tipTestAuthor)
	}},
}

func TestPullMerges_CombineBothSides(t *testing.T) {
	for _, tt := range pullMerges {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			if err := tt.merge(r); err != nil {
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

func TestPullMerges_RefuseConflictingEdits(t *testing.T) {
	for _, tt := range pullMerges {
		t.Run(tt.name, func(t *testing.T) {
			r := newTipTestRepo(t)
			tip := r.commit(t, "main", "f.txt", "main's f\n")
			if err := tt.merge(r); err == nil || !strings.Contains(err.Error(), "conflicting changes") {
				t.Errorf("err = %v, want a conflict error", err)
			}
			if got := branchTip(t, r.repo, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}
