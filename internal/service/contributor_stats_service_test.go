package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestContributorStatsService_IngestCommit_IsIdempotentBySha(t *testing.T) {
	db := testutil.OpenTestDB(t)

	statsStore := store.NewContributorStatsStore(db)
	userStore := store.NewUserStore(db)
	svc := service.NewContributorStatsService(statsStore, userStore)

	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, userID, "testuser_"+suffix, suffix)

	when := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	sha := "deadbeefcafe1234"

	if err := svc.IngestCommit(ctx, repoID, userID, when, sha, 50, 10); err != nil {
		t.Fatalf("first IngestCommit: %v", err)
	}
	if err := svc.IngestCommit(ctx, repoID, userID, when, sha, 50, 10); err != nil {
		t.Fatalf("second IngestCommit: %v", err)
	}

	rows, err := statsStore.ListForRepo(ctx, repoID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 aggregate row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Commits != 1 || rows[0].Additions != 50 || rows[0].Deletions != 10 {
		t.Errorf("idempotency violated: got commits=%d additions=%d deletions=%d, want 1/50/10",
			rows[0].Commits, rows[0].Additions, rows[0].Deletions)
	}

	var dayCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(commit_count), 0) FROM commit_day_counts WHERE repo_id = $1`, repoID,
	).Scan(&dayCount); err != nil {
		t.Fatalf("query day counts: %v", err)
	}
	if dayCount != 1 {
		t.Errorf("day counts: want sum 1, got %d", dayCount)
	}
}
