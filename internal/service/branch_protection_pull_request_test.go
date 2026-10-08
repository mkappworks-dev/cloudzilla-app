package service_test

// Integration tests for the require_pull_request rule flag.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"errors"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestRequirePullRequest_FlagRoundTrips(t *testing.T) {
	svc, repoID := newBPSvc(t)
	ctx := context.Background()
	bp := &model.BranchProtection{RepoID: repoID, Pattern: "main", RequirePullRequest: true}
	seedBranchProtection(t, svc, bp)

	rules, err := svc.List(ctx, repoID)
	if err != nil || len(rules) != 1 {
		t.Fatalf("List = %v, %v", rules, err)
	}
	if !rules[0].RequirePullRequest {
		t.Error("created rule lost RequirePullRequest")
	}

	if err := svc.Update(ctx, bp.ID, repoID, 0, nil, false, false); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rules, _ = svc.List(ctx, repoID)
	if rules[0].RequirePullRequest {
		t.Error("Update(false) left RequirePullRequest set")
	}
	if err := svc.Update(ctx, bp.ID, repoID, 0, nil, false, true); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rules, _ = svc.List(ctx, repoID)
	if !rules[0].RequirePullRequest {
		t.Error("Update(true) did not set RequirePullRequest")
	}
}

func TestRequirePullRequest_RefusesEveryDirectUpdate(t *testing.T) {
	svc, repoID := newBPSvc(t)
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "main", RequirePullRequest: true})
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "release/*", BlockForcePush: true})
	base, err := gogit.Init(memory.NewStorage(), nil)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	root := testutil.WriteCommit(t, base.Storer, "Root")
	next := testutil.WriteCommit(t, base.Storer, "Next", root)
	amended := testutil.WriteCommit(t, base.Storer, "Amended", root)

	tests := []struct {
		name string
		ref  string
		old  plumbing.Hash
		new  plumbing.Hash
		deny bool
	}{
		{"create", "refs/heads/main", plumbing.ZeroHash, root, true},
		{"fast-forward", "refs/heads/main", root, next, true},
		{"force push", "refs/heads/main", next, amended, true},
		{"delete", "refs/heads/main", next, plumbing.ZeroHash, true},
		{"unflagged rule", "refs/heads/release/1", root, next, false},
		{"unmatched branch", "refs/heads/topic", root, next, false},
		{"tag named like the branch", "refs/tags/main", root, next, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counter := &readCounter{Storer: base.Storer}
			repo, err := gogit.Open(counter, nil)
			if err != nil {
				t.Fatalf("open repo: %v", err)
			}
			cmd := &packp.Command{Name: plumbing.ReferenceName(tc.ref), Old: tc.old, New: tc.new}

			err = svc.CheckPushCommand(context.Background(), repoID, repo, cmd)
			if !tc.deny {
				if err != nil {
					t.Fatalf("CheckPushCommand = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, service.ErrPushRequiresPR) {
				t.Fatalf("CheckPushCommand = %v, want ErrPushRequiresPR", err)
			}
			if !strings.Contains(err.Error(), `"main"`) {
				t.Errorf("error %q doesn't name the rule", err)
			}
			if counter.reads != 0 {
				t.Errorf("read %d objects; the refusal needs no history", counter.reads)
			}
		})
	}
}

func TestRequirePullRequest_RefusesBranchDeletes(t *testing.T) {
	svc, repoID := newBPSvc(t)
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "main", RequirePullRequest: true})

	if err := svc.CheckDelete(context.Background(), repoID, "main"); !errors.Is(err, service.ErrPushRequiresPR) {
		t.Errorf("CheckDelete(main) = %v, want ErrPushRequiresPR", err)
	}
	if err := svc.CheckDelete(context.Background(), repoID, "topic"); err != nil {
		t.Errorf("CheckDelete(topic) = %v, want nil", err)
	}
}

// A pull request merge is how changes reach a branch that requires one.
func TestRequirePullRequest_MergesStillPass(t *testing.T) {
	svc, repoID := newBPSvc(t)
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "main", RequirePullRequest: true})

	pr := &model.PullRequest{ID: -1, BaseBranch: "main"}
	if err := svc.CheckMerge(context.Background(), repoID, pr, ""); err != nil {
		t.Errorf("CheckMerge = %v, want nil", err)
	}
}

func TestCheckWebCommit(t *testing.T) {
	svc, repoID := newBPSvc(t)
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "main", RequirePullRequest: true})
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "release", BlockForcePush: true, RequireReviewCount: 2})

	err := svc.CheckWebCommit(context.Background(), repoID, "main")
	if !errors.Is(err, service.ErrPushRequiresPR) || !strings.Contains(err.Error(), `"main"`) {
		t.Errorf("CheckWebCommit(main) = %v, want ErrPushRequiresPR naming the rule", err)
	}
	for _, branch := range []string{"release", "topic"} {
		if err := svc.CheckWebCommit(context.Background(), repoID, branch); err != nil {
			t.Errorf("CheckWebCommit(%s) = %v, want nil", branch, err)
		}
	}
}
