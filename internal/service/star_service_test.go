package service_test

// Integration tests for StarService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

// newStarSvc builds a StarService backed by the test database and seeds an owner
// and repo. Returns the service, repoID, and a second user ID for starring.
func newStarSvc(t *testing.T) (*service.StarService, int64, int64, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	starrerID := testutil.SeedUser(t, db, "starrer_"+suffix)
	svc := service.NewStarService(store.NewStarStore(db), store.NewRepoStore(db), store.NewUserStore(db))
	return svc, repoID, starrerID, ownerName, repoName
}

// TestStarService_Star_Succeeds verifies that Star does not return an error and
// the repo's star count increases by one after starring.
func TestStarService_Star_Succeeds(t *testing.T) {
	svc, repoID, starrerID, owner, repo := newStarSvc(t)

	before, err := svc.GetStarCount(context.Background(), repoID)
	if err != nil {
		t.Fatalf("GetStarCount before: %v", err)
	}

	if err := svc.Star(context.Background(), owner, repo, starrerID); err != nil {
		t.Fatalf("Star: %v", err)
	}

	after, err := svc.GetStarCount(context.Background(), repoID)
	if err != nil {
		t.Fatalf("GetStarCount after: %v", err)
	}
	if after <= before {
		t.Errorf("star count must increase after Star: before=%d after=%d", before, after)
	}
}

// TestStarService_IsStarred_TrueAfterStar verifies that IsStarred returns true
// after the user has starred the repository.
func TestStarService_IsStarred_TrueAfterStar(t *testing.T) {
	svc, repoID, starrerID, owner, repo := newStarSvc(t)

	if err := svc.Star(context.Background(), owner, repo, starrerID); err != nil {
		t.Fatalf("Star: %v", err)
	}

	starred, err := svc.IsStarred(context.Background(), repoID, starrerID)
	if err != nil {
		t.Fatalf("IsStarred: %v", err)
	}
	if !starred {
		t.Error("IsStarred must return true after starring")
	}
}

// TestStarService_Unstar_DecreaseCount verifies that Unstar removes the star so
// the count returns to its pre-star value and IsStarred returns false.
func TestStarService_Unstar_DecreaseCount(t *testing.T) {
	svc, repoID, starrerID, owner, repo := newStarSvc(t)

	if err := svc.Star(context.Background(), owner, repo, starrerID); err != nil {
		t.Fatalf("Star: %v", err)
	}
	if err := svc.Unstar(context.Background(), owner, repo, starrerID); err != nil {
		t.Fatalf("Unstar: %v", err)
	}

	starred, err := svc.IsStarred(context.Background(), repoID, starrerID)
	if err != nil {
		t.Fatalf("IsStarred after unstar: %v", err)
	}
	if starred {
		t.Error("IsStarred must return false after unstarring")
	}
}

// TestStarService_IsStarred_FalseForDifferentUser verifies that starring a repo by
// one user does not affect IsStarred for a different user (stars are per-user).
func TestStarService_IsStarred_FalseForDifferentUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	starrerID := testutil.SeedUser(t, db, "s1_"+suffix)
	otherID := testutil.SeedUser(t, db, "s2_"+suffix)

	svc := service.NewStarService(store.NewStarStore(db), store.NewRepoStore(db), store.NewUserStore(db))

	if err := svc.Star(context.Background(), ownerName, repoName, starrerID); err != nil {
		t.Fatalf("Star: %v", err)
	}

	starred, err := svc.IsStarred(context.Background(), repoID, otherID)
	if err != nil {
		t.Fatalf("IsStarred for other user: %v", err)
	}
	if starred {
		t.Error("IsStarred must return false for a user who has not starred the repo")
	}
}
