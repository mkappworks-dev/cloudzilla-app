package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestContributorStatsService_ForRepo_RanksContributorsAndAlignsWeeklyTimelines(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewContributorStatsService(store.NewContributorStatsStore(db), store.NewUserStore(db))
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	alice := testutil.SeedUser(t, db, "a"+suffix)
	bob := testutil.SeedUser(t, db, "b"+suffix)
	repoID := testutil.SeedRepo(t, db, alice, "testuser_a"+suffix, suffix)

	// Mondays: 4 May, 11 May and 18 May 2026.
	day := func(d int) time.Time { return time.Date(2026, 5, d, 10, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		user         int64
		when         time.Time
		sha          string
		adds, delete int
	}{
		{alice, day(5), "a1", 10, 1},
		{alice, day(6), "a2", 20, 2},
		{alice, day(20), "a3", 5, 0},
		{bob, day(13), "b1", 7, 7},
	} {
		if err := svc.IngestCommit(ctx, repoID, c.user, c.when, c.sha, c.adds, c.delete); err != nil {
			t.Fatalf("IngestCommit %s: %v", c.sha, err)
		}
	}

	got, err := svc.ForRepo(ctx, repoID)
	if err != nil {
		t.Fatalf("ForRepo: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("contributors = %+v; want 2", got)
	}
	a, b := got[0], got[1]
	if a.Username != "testuser_a"+suffix || a.Commits != 3 || a.Additions != 35 || a.Deletions != 3 {
		t.Errorf("first = %+v; want alice with 3 commits, +35 -3 (most commits first)", a)
	}
	if want := []int{2, 0, 1}; !slices.Equal(a.Timeline, want) {
		t.Errorf("alice timeline = %v; want %v", a.Timeline, want)
	}
	if b.Username != "testuser_b"+suffix || b.Commits != 1 || b.Additions != 7 || b.Deletions != 7 {
		t.Errorf("second = %+v; want bob with 1 commit, +7 -7", b)
	}
	if want := []int{0, 1, 0}; !slices.Equal(b.Timeline, want) {
		t.Errorf("bob timeline = %v; want %v (every contributor spans the same weeks)", b.Timeline, want)
	}
}

func TestContributorStatsService_ForRepo_NoCommitsIsEmpty(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewContributorStatsService(store.NewContributorStatsStore(db), store.NewUserStore(db))
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, owner, "testuser_"+suffix, suffix)

	got, err := svc.ForRepo(context.Background(), repoID)
	if err != nil || len(got) != 0 {
		t.Errorf("ForRepo = %+v, %v; want none", got, err)
	}
}

func TestContributorStatsService_ForRepo_ReportsStoreErrors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewContributorStatsService(store.NewContributorStatsStore(db), store.NewUserStore(db))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.ForRepo(ctx, 1); err == nil {
		t.Error("want an error from a cancelled context")
	}
}
