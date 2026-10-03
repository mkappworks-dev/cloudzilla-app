package service_test

// Integration tests for BranchProtectionService.CheckPush, CheckPushCommand
// and CheckMerge.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"errors"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newBPSvc builds a BranchProtectionService backed by the test database.
func newBPSvc(t *testing.T) (*service.BranchProtectionService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	svc := service.NewBranchProtectionService(
		store.NewBranchProtectionStore(db),
		store.NewPullReviewStore(db),
		store.NewCommitStatusStore(db),
	)
	return svc, repoID
}

// seedBranchProtection inserts a branch protection rule and registers cleanup.
func seedBranchProtection(t *testing.T, svc *service.BranchProtectionService, bp *model.BranchProtection) {
	t.Helper()
	if err := svc.Create(context.Background(), bp); err != nil {
		t.Fatalf("seedBranchProtection: %v", err)
	}
}

// --- CheckPush tests ---

// TestCheckPush_NoRule_Passes verifies that CheckPush returns nil when no branch
// protection rule matches the target branch (pushes are unrestricted by default).
func TestCheckPush_NoRule_Passes(t *testing.T) {
	svc, repoID := newBPSvc(t)
	err := svc.CheckPush(context.Background(), repoID, "main", false)
	if err != nil {
		t.Errorf("CheckPush with no rule must return nil, got %v", err)
	}
}

// TestCheckPush_ForcePushBlocked_ReturnsError verifies that CheckPush returns
// ErrForcePushBlocked when the matching rule has BlockForcePush=true and the
// push is a force push.
func TestCheckPush_ForcePushBlocked_ReturnsError(t *testing.T) {
	svc, repoID := newBPSvc(t)
	bp := &model.BranchProtection{
		RepoID:         repoID,
		Pattern:        "main",
		BlockForcePush: true,
	}
	seedBranchProtection(t, svc, bp)

	err := svc.CheckPush(context.Background(), repoID, "main", true)
	if !errors.Is(err, service.ErrForcePushBlocked) {
		t.Errorf("want ErrForcePushBlocked, got %v", err)
	}
}

// TestCheckPush_ForcePushAllowed_Passes verifies that a force push is allowed when
// the matching rule does not block force pushes (BlockForcePush=false).
func TestCheckPush_ForcePushAllowed_Passes(t *testing.T) {
	svc, repoID := newBPSvc(t)
	bp := &model.BranchProtection{
		RepoID:         repoID,
		Pattern:        "main",
		BlockForcePush: false,
	}
	seedBranchProtection(t, svc, bp)

	err := svc.CheckPush(context.Background(), repoID, "main", true)
	if err != nil {
		t.Errorf("CheckPush must pass when BlockForcePush=false, got %v", err)
	}
}

// TestCheckPush_NormalPush_Passes verifies that a normal (non-force) push is always
// allowed even when BlockForcePush=true (the rule only blocks force pushes).
func TestCheckPush_NormalPush_Passes(t *testing.T) {
	svc, repoID := newBPSvc(t)
	bp := &model.BranchProtection{
		RepoID:         repoID,
		Pattern:        "main",
		BlockForcePush: true,
	}
	seedBranchProtection(t, svc, bp)

	err := svc.CheckPush(context.Background(), repoID, "main", false)
	if err != nil {
		t.Errorf("normal push must pass even with BlockForcePush=true, got %v", err)
	}
}

// TestCheckPush_UnmatchedBranch_Passes verifies that a push to a branch not covered
// by any protection rule is always allowed.
func TestCheckPush_UnmatchedBranch_Passes(t *testing.T) {
	svc, repoID := newBPSvc(t)
	// Rule only covers "main", not "feature".
	bp := &model.BranchProtection{
		RepoID:         repoID,
		Pattern:        "main",
		BlockForcePush: true,
	}
	seedBranchProtection(t, svc, bp)

	err := svc.CheckPush(context.Background(), repoID, "feature/my-branch", true)
	if err != nil {
		t.Errorf("push to unmatched branch must pass, got %v", err)
	}
}

type readCounter struct {
	storage.Storer
	reads int
}

func (c *readCounter) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	c.reads++
	return c.Storer.EncodedObject(t, h)
}

// Telling a force push from a fast-forward walks history inside receive-pack,
// so a push that no rule blocks as a force push mustn't pay for the walk.
func TestCheckPushCommand_ReadsHistoryOnlyForARuleThatBlocksForcePushes(t *testing.T) {
	svc, repoID := newBPSvc(t)
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "main", BlockForcePush: true})
	seedBranchProtection(t, svc, &model.BranchProtection{RepoID: repoID, Pattern: "release", RequireReviewCount: 1})
	base, err := gogit.Init(memory.NewStorage(), nil)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	fork := testutil.WriteCommit(t, base.Storer, "Fork")
	draft := testutil.WriteCommit(t, base.Storer, "Draft", fork)
	amended := testutil.WriteCommit(t, base.Storer, "Amended draft", fork)

	tests := []struct {
		name, branch string
		want         error
		readsHistory bool
	}{
		{"no rule", "topic", nil, false},
		{"rule allowing force pushes", "release", nil, false},
		// Proves the counter sees the walk; without it, zero reads proves nothing.
		{"rule blocking force pushes", "main", service.ErrForcePushBlocked, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counter := &readCounter{Storer: base.Storer}
			repo, err := gogit.Open(counter, nil)
			if err != nil {
				t.Fatalf("open repo: %v", err)
			}
			cmd := &packp.Command{Name: plumbing.NewBranchReferenceName(tc.branch), Old: draft, New: amended}

			if err := svc.CheckPushCommand(context.Background(), repoID, repo, cmd); !errors.Is(err, tc.want) {
				t.Errorf("CheckPushCommand = %v, want %v", err, tc.want)
			}
			if got := counter.reads > 0; got != tc.readsHistory {
				t.Errorf("read %d objects, want history read = %v", counter.reads, tc.readsHistory)
			}
		})
	}
}

// --- CheckMerge tests ---

// TestCheckMerge_NoRule_Passes verifies that CheckMerge returns nil when no branch
// protection rule covers the PR's base branch.
func TestCheckMerge_NoRule_Passes(t *testing.T) {
	svc, repoID := newBPSvc(t)
	pr := &model.PullRequest{
		ID:         -1, // non-existent PR — no reviews will be found
		BaseBranch: "main",
	}
	err := svc.CheckMerge(context.Background(), repoID, pr, "")
	if err != nil {
		t.Errorf("CheckMerge with no rule must return nil, got %v", err)
	}
}

// TestCheckMerge_InsufficientReviews_ReturnsError verifies that CheckMerge returns
// ErrInsufficientReviews when the rule requires more approvals than are present.
func TestCheckMerge_InsufficientReviews_ReturnsError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	svc := service.NewBranchProtectionService(
		store.NewBranchProtectionStore(db),
		store.NewPullReviewStore(db),
		store.NewCommitStatusStore(db),
	)

	// Require 2 approvals.
	bp := &model.BranchProtection{
		RepoID:             repoID,
		Pattern:            "main",
		RequireReviewCount: 2,
	}
	if err := svc.Create(context.Background(), bp); err != nil {
		t.Fatalf("create protection rule: %v", err)
	}

	// PR with 0 approvals.
	pr := &model.PullRequest{
		ID:         -999, // no reviews seeded
		BaseBranch: "main",
	}
	err := svc.CheckMerge(context.Background(), repoID, pr, "")
	if !errors.Is(err, service.ErrInsufficientReviews) {
		t.Errorf("want ErrInsufficientReviews, got %v", err)
	}
}

// TestCheckMerge_SufficientReviews_Passes verifies that CheckMerge returns nil when
// the number of approvals meets the required review count.
func TestCheckMerge_SufficientReviews_Passes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	svc := service.NewBranchProtectionService(
		store.NewBranchProtectionStore(db),
		store.NewPullReviewStore(db),
		store.NewCommitStatusStore(db),
	)

	// Require 1 approval.
	bp := &model.BranchProtection{
		RepoID:             repoID,
		Pattern:            "main",
		RequireReviewCount: 1,
	}
	if err := svc.Create(context.Background(), bp); err != nil {
		t.Fatalf("create protection rule: %v", err)
	}

	// Seed a PR and an 'approved' review.
	var prID int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch, is_draft)
		 VALUES ($1, 1, $2, 'test', '', 'open', 'feature', 'main', false) RETURNING id`,
		repoID, ownerID,
	).Scan(&prID); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM pull_reviews WHERE pull_id = $1`, prID)
		testutil.Exec(t, db, `DELETE FROM pull_requests WHERE id = $1`, prID)
	})

	reviewerID := testutil.SeedUser(t, db, "reviewer_"+suffix)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body)
		 VALUES ($1, $2, $3, 'reviewer', 'approved', '')`,
		prID, repoID, reviewerID,
	); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	pr := &model.PullRequest{ID: prID, BaseBranch: "main"}
	err := svc.CheckMerge(context.Background(), repoID, pr, "")
	if err != nil {
		t.Errorf("CheckMerge must pass with sufficient approvals, got %v", err)
	}
}

// TestCheckMerge_StatusCheckMissing_ReturnsError verifies that CheckMerge returns
// ErrStatusCheckFailed when a required CI context has no status record for headSHA.
func TestCheckMerge_StatusCheckMissing_ReturnsError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	svc := service.NewBranchProtectionService(
		store.NewBranchProtectionStore(db),
		store.NewPullReviewStore(db),
		store.NewCommitStatusStore(db),
	)

	// Require "ci/test" status check.
	bp := &model.BranchProtection{
		RepoID:              repoID,
		Pattern:             "main",
		RequireStatusChecks: model.StringSlice{"ci/test"},
	}
	if err := svc.Create(context.Background(), bp); err != nil {
		t.Fatalf("create protection rule: %v", err)
	}

	// No commit status seeded for this SHA.
	pr := &model.PullRequest{ID: -1, BaseBranch: "main"}
	err := svc.CheckMerge(context.Background(), repoID, pr, "deadbeef00000000000000000000000000000000")
	if !errors.Is(err, service.ErrStatusCheckFailed) {
		t.Errorf("want ErrStatusCheckFailed for missing status, got %v", err)
	}
}

// TestCheckMerge_StatusCheckFailed_ReturnsError verifies that CheckMerge returns
// ErrStatusCheckFailed when a required CI context exists but its state is not "success".
func TestCheckMerge_StatusCheckFailed_ReturnsError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	svc := service.NewBranchProtectionService(
		store.NewBranchProtectionStore(db),
		store.NewPullReviewStore(db),
		store.NewCommitStatusStore(db),
	)

	bp := &model.BranchProtection{
		RepoID:              repoID,
		Pattern:             "main",
		RequireStatusChecks: model.StringSlice{"ci/test"},
	}
	if err := svc.Create(context.Background(), bp); err != nil {
		t.Fatalf("create protection rule: %v", err)
	}

	// Seed a "failure" commit status for the required context.
	headSHA := "failsha0000000000000000000000000000000000"
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO commit_statuses (repo_id, sha, context, state, target_url, description, creator_id)
		 VALUES ($1, $2, 'ci/test', 'failure', '', '', $3)`,
		repoID, headSHA, ownerID,
	); err != nil {
		t.Fatalf("seed commit status: %v", err)
	}
	t.Cleanup(func() {
		testutil.Exec(t, db,
			`DELETE FROM commit_statuses WHERE repo_id = $1 AND sha = $2`, repoID, headSHA)
	})

	pr := &model.PullRequest{ID: -1, BaseBranch: "main"}
	err := svc.CheckMerge(context.Background(), repoID, pr, headSHA)
	if !errors.Is(err, service.ErrStatusCheckFailed) {
		t.Errorf("want ErrStatusCheckFailed for failed status, got %v", err)
	}
}

// TestCheckMerge_AllStatusChecksPassing_Passes verifies that CheckMerge returns nil
// when all required CI contexts have a "success" state for headSHA.
func TestCheckMerge_AllStatusChecksPassing_Passes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	svc := service.NewBranchProtectionService(
		store.NewBranchProtectionStore(db),
		store.NewPullReviewStore(db),
		store.NewCommitStatusStore(db),
	)

	bp := &model.BranchProtection{
		RepoID:              repoID,
		Pattern:             "main",
		RequireStatusChecks: model.StringSlice{"ci/test", "ci/lint"},
	}
	if err := svc.Create(context.Background(), bp); err != nil {
		t.Fatalf("create protection rule: %v", err)
	}

	// Seed "success" statuses for both required contexts.
	headSHA := "goodsha0000000000000000000000000000000000"
	for _, ctx := range []string{"ci/test", "ci/lint"} {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO commit_statuses (repo_id, sha, context, state, target_url, description, creator_id)
			 VALUES ($1, $2, $3, 'success', '', '', $4)`,
			repoID, headSHA, ctx, ownerID,
		); err != nil {
			t.Fatalf("seed commit status %q: %v", ctx, err)
		}
	}
	t.Cleanup(func() {
		testutil.Exec(t, db,
			`DELETE FROM commit_statuses WHERE repo_id = $1 AND sha = $2`, repoID, headSHA)
	})

	pr := &model.PullRequest{ID: -1, BaseBranch: "main"}
	err := svc.CheckMerge(context.Background(), repoID, pr, headSHA)
	if err != nil {
		t.Errorf("CheckMerge must pass when all status checks succeed, got %v", err)
	}
}
