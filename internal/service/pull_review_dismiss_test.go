package service_test

// Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type dismissFixture struct {
	svc                *service.PullReviewService
	owner, repo        string
	pull               *model.PullRequest
	reviewer, outsider string
	reviewerID         int64
}

func newDismissFixture(t *testing.T, db *sql.DB) dismissFixture {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)
	reviewerID := testutil.SeedUser(t, db, suffix+"_rv")
	owner, repo := "testuser_"+suffix, "testrepo_"+suffix
	testutil.SeedRepo(t, db, authorID, owner, suffix)
	repoSvc := service.NewRepoService(
		store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db),
		nil, nil, config.GitConfig{},
	)
	pr, err := service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc).
		Create(context.Background(), owner, repo, authorID, "PR", "", "feature-branch", "main", false, nil)
	if err != nil {
		t.Fatalf("Create PR: %v", err)
	}
	return dismissFixture{
		svc:        service.NewPullReviewService(store.NewPullReviewStore(db), store.NewPullStore(db), store.NewRepoStore(db), nil),
		owner:      owner,
		repo:       repo,
		pull:       pr,
		reviewer:   owner + "_rv",
		reviewerID: reviewerID,
	}
}

func (f dismissFixture) submit(t *testing.T, state, body string) *model.PullReview {
	t.Helper()
	r, err := f.svc.SubmitReview(context.Background(), f.owner, f.repo, f.pull.Number, f.reviewerID, f.reviewer, state, body)
	if err != nil {
		t.Fatalf("submit %s: %v", state, err)
	}
	return r
}

func TestDismiss_ClearsTheMergeBlock(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newDismissFixture(t, db)
	ctx := context.Background()
	rv := f.submit(t, "changes_requested", "fix the tests")
	if ok, _, err := f.svc.CanMerge(ctx, f.pull.ID); err != nil || ok {
		t.Fatalf("CanMerge before = %v, %v; want blocked", ok, err)
	}

	got, err := f.svc.Dismiss(ctx, f.owner, f.repo, f.pull.Number, rv.ID, "admin_user", "  superseded by the rewrite  ")
	if err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if got.State != model.PRReviewDismissed || got.DismissedBy != "admin_user" || got.DismissedReason != "superseded by the rewrite" || got.DismissedAt == nil {
		t.Errorf("dismissed review = %+v", got)
	}
	if got.Body != "fix the tests" {
		t.Errorf("body = %q, want the reviewer's words kept", got.Body)
	}
	if ok, _, err := f.svc.CanMerge(ctx, f.pull.ID); err != nil || !ok {
		t.Errorf("CanMerge after = %v, %v; want unblocked", ok, err)
	}
	if blockers, err := f.svc.ChangesRequestedBy(ctx, f.pull.ID); err != nil || len(blockers) != 0 {
		t.Errorf("ChangesRequestedBy after = %v, %v; want none", blockers, err)
	}

	if _, err := f.svc.Dismiss(ctx, f.owner, f.repo, f.pull.Number, rv.ID, "admin_user", "again"); !errors.Is(err, service.ErrReviewNotDismissible) {
		t.Errorf("second dismissal: want ErrReviewNotDismissible, got %v", err)
	}
}

func TestDismiss_NeedsAReasonAndAStandingRequest(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newDismissFixture(t, db)
	ctx := context.Background()
	rv := f.submit(t, "changes_requested", "fix the tests")

	for _, reason := range []string{"", "   "} {
		if _, err := f.svc.Dismiss(ctx, f.owner, f.repo, f.pull.Number, rv.ID, "admin_user", reason); !errors.Is(err, service.ErrDismissReasonRequired) {
			t.Errorf("reason %q: want ErrDismissReasonRequired, got %v", reason, err)
		}
	}
	if _, err := f.svc.Dismiss(ctx, f.owner, f.repo, f.pull.Number, rv.ID, "admin_user", strings.Repeat("x", service.MaxDismissReason+1)); err == nil {
		t.Error("an over-long reason was accepted")
	}
	if _, err := f.svc.Dismiss(ctx, f.owner, f.repo, f.pull.Number, rv.ID+1000, "admin_user", "why"); !errors.Is(err, service.ErrReviewNotFound) {
		t.Errorf("unknown review: want ErrReviewNotFound, got %v", err)
	}

	approved := f.submit(t, "approved", "")
	if _, err := f.svc.Dismiss(ctx, f.owner, f.repo, f.pull.Number, approved.ID, "admin_user", "why"); !errors.Is(err, service.ErrReviewNotDismissible) {
		t.Errorf("approval: want ErrReviewNotDismissible, got %v", err)
	}
}

func TestDismiss_ReviewOfAnotherPullIsNotFound(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newDismissFixture(t, db)
	other := newDismissFixture(t, db)
	rv := other.submit(t, "changes_requested", "no")

	if _, err := f.svc.Dismiss(context.Background(), f.owner, f.repo, f.pull.Number, rv.ID, "admin_user", "why"); !errors.Is(err, service.ErrReviewNotFound) {
		t.Errorf("want ErrReviewNotFound, got %v", err)
	}
	if ok, _, _ := other.svc.CanMerge(context.Background(), other.pull.ID); ok {
		t.Error("the other pull's request for changes was dismissed through this pull")
	}
}

func TestDismiss_ANewReviewReplacesTheDismissal(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newDismissFixture(t, db)
	ctx := context.Background()
	rv := f.submit(t, "changes_requested", "fix the tests")
	if _, err := f.svc.Dismiss(ctx, f.owner, f.repo, f.pull.Number, rv.ID, "admin_user", "stale"); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}

	again := f.submit(t, "changes_requested", "still broken")
	if again.State != model.PRReviewChangesRequested {
		t.Fatalf("state = %s, want a fresh request for changes", again.State)
	}
	reviews, err := f.svc.ListByPull(ctx, f.owner, f.repo, f.pull.Number)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("ListByPull = %v, %v", reviews, err)
	}
	if r := reviews[0]; r.DismissedBy != "" || r.DismissedReason != "" || r.DismissedAt != nil {
		t.Errorf("dismissal left on a new verdict: %+v", r)
	}
	if ok, _, _ := f.svc.CanMerge(ctx, f.pull.ID); ok {
		t.Error("a new request for changes does not block the merge")
	}
}
