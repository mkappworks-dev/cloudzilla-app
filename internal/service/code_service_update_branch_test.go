package service

import (
	"context"
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestUpdatePullBranch_MergesBaseIntoHead(t *testing.T) {
	r := newDivergedPull(t)
	oldHead, mainTip := branchTip(t, r.repo, "feature"), branchTip(t, r.repo, "main")

	upd, err := r.svc.UpdatePullBranch("alice", "pulls", "main", "feature", oldHead, tipTestAuthor)
	if err != nil {
		t.Fatalf("UpdatePullBranch: %v", err)
	}
	if upd.Branch != "feature" || upd.Old != oldHead || upd.New != branchTip(t, r.repo, "feature") {
		t.Fatalf("RefUpdate = %+v, want feature moved from %s to its tip", upd, oldHead)
	}
	if got := branchTip(t, r.repo, "main"); got != mainTip {
		t.Errorf("main moved to %s, want it untouched at %s", got, mainTip)
	}

	commit, err := r.repo.CommitObject(upd.New)
	if err != nil {
		t.Fatalf("read update commit: %v", err)
	}
	if want := []plumbing.Hash{oldHead, mainTip}; len(commit.ParentHashes) != 2 || commit.ParentHashes[0] != want[0] || commit.ParentHashes[1] != want[1] {
		t.Errorf("parents = %v, want head then base %v", commit.ParentHashes, want)
	}
	if want := DefaultUpdateBranchMessage("main", "feature"); commit.Message != want {
		t.Errorf("message = %q, want %q", commit.Message, want)
	}
	for _, path := range []string{"a.txt", "b.txt"} {
		if _, err := commit.File(path); err != nil {
			t.Errorf("updated head lacks %s: %v", path, err)
		}
	}

	mg, err := r.svc.Mergeability(context.Background(), "alice", "pulls", "main", "feature")
	if err != nil {
		t.Fatalf("Mergeability: %v", err)
	}
	if mg.Behind != 0 || mg.HasConflicts {
		t.Errorf("after the update: behind %d, conflicts %v; want 0, false", mg.Behind, mg.HasConflicts)
	}

	if _, err := r.svc.UpdatePullBranch("alice", "pulls", "main", "feature", upd.New, tipTestAuthor); !errors.Is(err, ErrBranchUpToDate) {
		t.Errorf("second update: want ErrBranchUpToDate, got %v", err)
	}
}

func TestUpdatePullBranch_ConflictLeavesHeadAlone(t *testing.T) {
	r := newDivergedPull(t)
	r.commit(t, "main", "a.txt", "one\nmain edit\n")
	head := branchTip(t, r.repo, "feature")

	if _, err := r.svc.UpdatePullBranch("alice", "pulls", "main", "feature", head, tipTestAuthor); err == nil {
		t.Fatal("conflicting update succeeded")
	}
	if got := branchTip(t, r.repo, "feature"); got != head {
		t.Errorf("feature moved to %s after a refused update, want %s", got, head)
	}
}

func TestUpdatePullBranch_RefusesAMovedHead(t *testing.T) {
	r := newDivergedPull(t)
	seen := branchTip(t, r.repo, "feature")
	r.commit(t, "feature", "late.txt", "pushed meanwhile\n")
	moved := branchTip(t, r.repo, "feature")

	if _, err := r.svc.UpdatePullBranch("alice", "pulls", "main", "feature", seen, tipTestAuthor); !errors.Is(err, ErrRefMoved) {
		t.Errorf("want ErrRefMoved, got %v", err)
	}
	if got := branchTip(t, r.repo, "feature"); got != moved {
		t.Errorf("feature = %s, want the pushed commit %s kept", got, moved)
	}
}
