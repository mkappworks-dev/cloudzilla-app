package store_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func utcDay(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestCommitStatsStore_UserQueries(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoA := testutil.SeedRepo(t, db, userID, "u_"+suffix, suffix+"_a")
	repoB := testutil.SeedRepo(t, db, userID, "u_"+suffix, suffix+"_b")
	deleted := testutil.SeedRepo(t, db, userID, "u_"+suffix, suffix+"_c")
	s := store.NewCommitStatsStore(db)

	rows := []struct {
		repo int64
		day  time.Time
		n    int
	}{
		{repoA, utcDay(2023, 12, 31), 1},
		{repoA, utcDay(2024, 1, 1), 2},
		{repoB, utcDay(2024, 1, 1), 3},
		{repoA, utcDay(2024, 6, 15), 4},
		{deleted, utcDay(2025, 3, 3), 9},
	}
	for _, r := range rows {
		if err := s.UpsertCount(ctx, r.repo, userID, r.day, r.n); err != nil {
			t.Fatalf("UpsertCount: %v", err)
		}
	}
	testutil.Exec(t, db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, deleted)

	t.Run("CountDistinctReposForUserSince skips soft-deleted repos and older days", func(t *testing.T) {
		tests := []struct {
			name  string
			since time.Time
			want  int
		}{
			{"all", utcDay(2023, 1, 1), 2},
			{"only repoA after cutoff", utcDay(2024, 2, 1), 1},
			{"deleted repo only", utcDay(2025, 1, 1), 0},
		}
		for _, tc := range tests {
			got, err := s.CountDistinctReposForUserSince(ctx, userID, tc.since)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got != tc.want {
				t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		}
	})

	t.Run("ListForUserBetween is inclusive and ordered", func(t *testing.T) {
		got, err := s.ListForUserBetween(ctx, userID, utcDay(2024, 1, 1), utcDay(2024, 6, 15))
		if err != nil {
			t.Fatalf("ListForUserBetween: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("want 3 rows (both bounds inclusive, 2023-12-31 and deleted repo excluded), got %d: %+v", len(got), got)
		}
		if !got[0].Day.Equal(utcDay(2024, 1, 1)) || !got[2].Day.Equal(utcDay(2024, 6, 15)) {
			t.Errorf("rows not ordered by day: %+v", got)
		}
		for _, c := range got {
			if c.UserID != userID {
				t.Errorf("UserID = %d, want %d", c.UserID, userID)
			}
		}
	})

	t.Run("ListForUserBetween empty range", func(t *testing.T) {
		got, err := s.ListForUserBetween(ctx, userID, utcDay(2030, 1, 1), utcDay(2030, 12, 31))
		if err != nil {
			t.Fatalf("ListForUserBetween: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("want none, got %+v", got)
		}
	})

	t.Run("YearsForUser is distinct, descending, and skips soft-deleted repos", func(t *testing.T) {
		got, err := s.YearsForUser(ctx, userID)
		if err != nil {
			t.Fatalf("YearsForUser: %v", err)
		}
		if want := []int{2024, 2023}; !slices.Equal(got, want) {
			t.Errorf("years = %v, want %v", got, want)
		}
	})

	t.Run("YearsForUser unknown user", func(t *testing.T) {
		got, err := s.YearsForUser(ctx, -1)
		if err != nil || len(got) != 0 {
			t.Errorf("got %v, %v; want empty, nil", got, err)
		}
	})
}

func TestCommitStatsStore_HasRowsForRepoSince(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, userID, "u_"+suffix, suffix)
	s := store.NewCommitStatsStore(db)

	if err := s.UpsertCount(ctx, repoID, userID, utcDay(2024, 5, 5), 1); err != nil {
		t.Fatalf("UpsertCount: %v", err)
	}
	tests := []struct {
		name  string
		since time.Time
		want  bool
	}{
		{"since before row", utcDay(2024, 1, 1), true},
		{"since later the same day", utcDay(2024, 5, 5).Add(13 * time.Hour), true},
		{"since after row", utcDay(2024, 5, 6), false},
	}
	for _, tc := range tests {
		got, err := s.HasRowsForRepoSince(ctx, repoID, tc.since)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCommitStatsStore_RepoQueries(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	alice := testutil.SeedUser(t, db, suffix+"_a")
	bob := testutil.SeedUser(t, db, suffix+"_b")
	repoID := testutil.SeedRepo(t, db, alice, "u_"+suffix, suffix)
	s := store.NewCommitStatsStore(db)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	older := today.AddDate(0, 0, -21)
	for _, r := range []struct {
		user int64
		day  time.Time
		n    int
	}{{alice, today, 2}, {bob, today, 5}, {alice, older, 4}} {
		if err := s.UpsertCount(ctx, repoID, r.user, r.day, r.n); err != nil {
			t.Fatalf("UpsertCount: %v", err)
		}
	}

	t.Run("ListForRepoSince sums across users per day", func(t *testing.T) {
		got, err := s.ListForRepoSince(ctx, repoID, older)
		if err != nil {
			t.Fatalf("ListForRepoSince: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 days, got %+v", got)
		}
		if got[0].CommitCount != 4 || got[1].CommitCount != 7 {
			t.Errorf("counts = %d, %d; want 4, 7", got[0].CommitCount, got[1].CommitCount)
		}
		for _, c := range got {
			if c.UserID != 0 || c.RepoID != repoID {
				t.Errorf("UserID/RepoID = %d/%d, want 0/%d", c.UserID, c.RepoID, repoID)
			}
		}
	})

	t.Run("ListForRepoSince respects cutoff", func(t *testing.T) {
		got, err := s.ListForRepoSince(ctx, repoID, today)
		if err != nil {
			t.Fatalf("ListForRepoSince: %v", err)
		}
		if len(got) != 1 || got[0].CommitCount != 7 {
			t.Errorf("got %+v, want one day totalling 7", got)
		}
	})

	t.Run("WeeklyForRepo buckets and clamps weeks", func(t *testing.T) {
		got, err := s.WeeklyForRepo(ctx, repoID, 8)
		if err != nil {
			t.Fatalf("WeeklyForRepo: %v", err)
		}
		if len(got) != 8 {
			t.Fatalf("want 8 buckets, got %d", len(got))
		}
		total := 0
		for _, n := range got {
			total += n
		}
		if total != 11 || got[7] < 7 {
			t.Errorf("buckets = %v; want total 11 with current week >= 7", got)
		}
		for in, want := range map[int]int{0: 1, -3: 1, 500: 104} {
			b, err := s.WeeklyForRepo(ctx, repoID, in)
			if err != nil {
				t.Fatalf("WeeklyForRepo(%d): %v", in, err)
			}
			if len(b) != want {
				t.Errorf("WeeklyForRepo(%d) len = %d, want %d", in, len(b), want)
			}
		}
	})
}
