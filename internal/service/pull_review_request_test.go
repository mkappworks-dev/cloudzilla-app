package service_test

// Integration tests for PullReviewService.RequestReviewers.
// Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newReviewRequestFixture(t *testing.T) (
	svc *service.PullReviewService,
	ownerName, repoName string,
	prNumber int,
	reviewer1, reviewer2 model.User,
) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)

	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName = "testuser_" + suffix
	repoName = "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	r1ID := testutil.SeedUser(t, db, suffix+"_r1")
	r2ID := testutil.SeedUser(t, db, suffix+"_r2")

	reviewer1 = model.User{ID: r1ID, Username: "testuser_" + suffix + "_r1"}
	reviewer2 = model.User{ID: r2ID, Username: "testuser_" + suffix + "_r2"}

	repoSvc := service.NewRepoService(
		store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db),
		nil, nil, nil, config.GitConfig{},
	)
	pullSvc := service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc)

	ctx := context.Background()
	pr, err := pullSvc.Create(ctx, ownerName, repoName, ownerID,
		"Request reviewers PR", "", "feature-branch", "main", false)
	if err != nil {
		t.Fatalf("Create PR: %v", err)
	}
	prNumber = pr.Number

	svc = service.NewPullReviewService(
		store.NewPullReviewStore(db),
		store.NewPullStore(db),
		store.NewRepoStore(db),
		nil,
	)
	return
}

func TestRequestReviewers_CreatesPendingReviews(t *testing.T) {
	svc, ownerName, repoName, prNumber, r1, r2 := newReviewRequestFixture(t)
	ctx := context.Background()

	if err := svc.RequestReviewers(ctx, ownerName, repoName, prNumber, []model.User{r1, r2}); err != nil {
		t.Fatalf("RequestReviewers: %v", err)
	}

	reviews, err := svc.ListByPull(ctx, ownerName, repoName, prNumber)
	if err != nil {
		t.Fatalf("ListByPull: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(reviews))
	}

	authorIDs := map[int64]model.PRReviewState{}
	for _, rev := range reviews {
		authorIDs[rev.AuthorID] = rev.State
	}
	for _, u := range []model.User{r1, r2} {
		state, ok := authorIDs[u.ID]
		if !ok {
			t.Errorf("no review found for user %d (%s)", u.ID, u.Username)
			continue
		}
		if state != model.PRReviewPending {
			t.Errorf("user %d: expected state %q, got %q", u.ID, model.PRReviewPending, state)
		}
	}
}

func TestRequestReviewers_Idempotent(t *testing.T) {
	svc, ownerName, repoName, prNumber, r1, r2 := newReviewRequestFixture(t)
	ctx := context.Background()

	if err := svc.RequestReviewers(ctx, ownerName, repoName, prNumber, []model.User{r1, r2}); err != nil {
		t.Fatalf("first RequestReviewers: %v", err)
	}
	if err := svc.RequestReviewers(ctx, ownerName, repoName, prNumber, []model.User{r1, r2}); err != nil {
		t.Fatalf("second RequestReviewers: %v", err)
	}

	reviews, err := svc.ListByPull(ctx, ownerName, repoName, prNumber)
	if err != nil {
		t.Fatalf("ListByPull: %v", err)
	}
	if len(reviews) != 2 {
		t.Errorf("expected 2 reviews after idempotent call, got %d", len(reviews))
	}
}

func TestRequestReviewers_DoesNotClobberExistingReview(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ctx := context.Background()

	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	approverID := testutil.SeedUser(t, db, suffix+"_approver")
	approver := model.User{ID: approverID, Username: "testuser_" + suffix + "_approver"}

	repoSvc := service.NewRepoService(
		store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db),
		nil, nil, nil, config.GitConfig{},
	)
	pullSvc := service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc)

	pr, err := pullSvc.Create(ctx, ownerName, repoName, ownerID,
		"Clobber test PR", "", "feature-clobber", "main", false)
	if err != nil {
		t.Fatalf("Create PR: %v", err)
	}

	reviewSvc := service.NewPullReviewService(
		store.NewPullReviewStore(db),
		store.NewPullStore(db),
		store.NewRepoStore(db),
		nil,
	)

	// The approver submits a real review first.
	_, err = reviewSvc.SubmitReview(ctx, ownerName, repoName, pr.Number,
		approverID, approver.Username, "approved", "LGTM")
	if err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}

	// Re-requesting the same reviewer must not overwrite the approved state.
	if err := reviewSvc.RequestReviewers(ctx, ownerName, repoName, pr.Number, []model.User{approver}); err != nil {
		t.Fatalf("RequestReviewers: %v", err)
	}

	reviews, err := reviewSvc.ListByPull(ctx, ownerName, repoName, pr.Number)
	if err != nil {
		t.Fatalf("ListByPull: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(reviews))
	}
	if reviews[0].State != model.PRReviewApproved {
		t.Errorf("approved review must not be clobbered; got state %q", reviews[0].State)
	}
}
