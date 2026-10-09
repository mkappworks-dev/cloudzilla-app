package service

import (
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
)

func TestCodeService_RefCreatorsRefuseInvalidNames(t *testing.T) {
	r := newTipTestRepo(t)
	for _, name := range []string{"a..b", "x y", "bad.lock", "end.", "-x"} {
		branch := plumbing.NewBranchReferenceName(name)
		tag := plumbing.NewTagReferenceName(name)

		if err := r.svc.CreateBranch(r.owner, r.name, name, "main"); !errors.Is(err, gitref.ErrInvalidName) {
			t.Errorf("CreateBranch(%q) = %v, want ErrInvalidName", name, err)
		}
		if _, err := r.svc.CommitFile(r.owner, r.name, name, "n.txt", []byte("n\n"), tipTestAuthor, "Add n.txt"); !errors.Is(err, gitref.ErrInvalidName) {
			t.Errorf("CommitFile onto new branch %q = %v, want ErrInvalidName", name, err)
		}
		if _, err := r.repo.Reference(branch, true); err == nil {
			t.Errorf("branch %q was written", name)
		}

		if name == "-x" {
			continue // a tag may start with a dash
		}
		if err := r.svc.CreateTag(r.owner, r.name, name, "main"); !errors.Is(err, gitref.ErrInvalidName) {
			t.Errorf("CreateTag(%q) = %v, want ErrInvalidName", name, err)
		}
		if _, err := r.repo.Reference(tag, true); err == nil {
			t.Errorf("tag %q was written", name)
		}
	}
}
