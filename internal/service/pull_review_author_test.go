package service_test

// Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSubmitReview_AuthorMayOnlyComment(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)
	owner, repo := "testuser_"+suffix, "testrepo_"+suffix
	testutil.SeedRepo(t, db, authorID, owner, suffix)

	repoSvc := service.NewRepoService(
		store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db),
		nil, nil, config.GitConfig{},
	)
	pr, err := service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc).
		Create(context.Background(), owner, repo, authorID, "Own PR", "", "feature-branch", "main", false, nil)
	if err != nil {
		t.Fatalf("Create PR: %v", err)
	}
	svc := service.NewPullReviewService(store.NewPullReviewStore(db), store.NewPullStore(db), store.NewRepoStore(db), nil)
	ctx := context.Background()

	for _, state := range []string{"approved", "changes_requested"} {
		if _, err := svc.SubmitReview(ctx, owner, repo, pr.Number, authorID, owner, state, ""); !errors.Is(err, service.ErrOwnPullVerdict) {
			t.Errorf("author %s: want ErrOwnPullVerdict, got %v", state, err)
		}
	}
	if _, err := svc.SubmitReview(ctx, owner, repo, pr.Number, authorID, owner, "bogus", ""); err == nil {
		t.Error("unknown state accepted")
	}
	if _, err := svc.SubmitReview(ctx, owner, repo, pr.Number, authorID, owner, "commented", "a note"); err != nil {
		t.Fatalf("author commented: %v", err)
	}
	reviews, err := svc.ListByPull(ctx, owner, repo, pr.Number)
	if err != nil {
		t.Fatalf("ListByPull: %v", err)
	}
	if len(reviews) != 1 || reviews[0].State != model.PRReviewCommented || reviews[0].Body != "a note" {
		t.Fatalf("want one stored commented review, got %+v", reviews)
	}
}
