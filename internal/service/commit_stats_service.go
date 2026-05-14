package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// CommitSample is one commit's contribution data for stats ingestion.
// AuthorEmail is matched against UserStore.GetByEmail to attribute the
// commit to a user; samples with no matching user are silently skipped.
type CommitSample struct {
	AuthorEmail string
	Time        time.Time
}

// CommitStatsService aggregates per-(user, day) commit counts that back
// the contribution heatmap.
type CommitStatsService struct {
	stats *store.CommitStatsStore
	users *store.UserStore
}

// NewCommitStatsService creates a CommitStatsService backed by the given stores.
func NewCommitStatsService(stats *store.CommitStatsStore, users *store.UserStore) *CommitStatsService {
	return &CommitStatsService{stats: stats, users: users}
}

// Ingest aggregates the given commit samples by (matched user, day) and
// upserts each bucket. Anonymous commits (no matching user email) are
// silently skipped — they do not contribute to any user's heatmap.
func (s *CommitStatsService) Ingest(ctx context.Context, repoID int64, samples []CommitSample) error {
	type key struct {
		userID int64
		day    time.Time
	}
	buckets := make(map[key]int)
	cache := make(map[string]int64)

	for _, c := range samples {
		userID, ok := cache[c.AuthorEmail]
		if !ok {
			u, err := s.users.GetByEmail(ctx, c.AuthorEmail)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					slog.Warn("commit stats: user lookup failed", "email", c.AuthorEmail, "error", err)
				}
				cache[c.AuthorEmail] = 0
				continue
			}
			if u == nil {
				cache[c.AuthorEmail] = 0
				continue
			}
			userID = u.ID
			cache[c.AuthorEmail] = userID
		}
		if userID == 0 {
			continue
		}
		day := c.Time.UTC().Truncate(24 * time.Hour)
		buckets[key{userID, day}]++
	}
	for k, count := range buckets {
		if err := s.stats.UpsertCount(ctx, repoID, k.userID, k.day, count); err != nil {
			return err
		}
	}
	return nil
}

// CommitsForUserSince returns the total commit count for the user across
// all repos over the past `days` days. Used by the home page stat strip.
func (s *CommitStatsService) CommitsForUserSince(ctx context.Context, userID int64, days int) (int, error) {
	since := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -days+1)
	rows, err := s.stats.ListForUserSince(ctx, userID, since)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, r := range rows {
		total += r.CommitCount
	}
	return total, nil
}

// LookbackForRepo returns per-day commit counts (summed across users)
// for the given repo over the past `days` days. Used by the repo About
// sidebar mini heatmap.
func (s *CommitStatsService) LookbackForRepo(ctx context.Context, repoID int64, days int) (map[time.Time]int, error) {
	since := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -days+1)
	rows, err := s.stats.ListForRepoSince(ctx, repoID, since)
	if err != nil {
		return nil, err
	}
	out := make(map[time.Time]int, days)
	for i := 0; i < days; i++ {
		out[since.AddDate(0, 0, i)] = 0
	}
	for _, r := range rows {
		out[r.Day.UTC().Truncate(24*time.Hour)] = r.CommitCount
	}
	return out, nil
}

// LookbackForUser returns per-day commit counts for the user over the past
// `days` days. The full window is materialized — days with zero commits
// get an explicit zero entry — so the heatmap can render a regular grid.
func (s *CommitStatsService) LookbackForUser(ctx context.Context, userID int64, days int) (map[time.Time]int, error) {
	since := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -days+1)
	rows, err := s.stats.ListForUserSince(ctx, userID, since)
	if err != nil {
		return nil, err
	}
	out := make(map[time.Time]int, days)
	for i := 0; i < days; i++ {
		out[since.AddDate(0, 0, i)] = 0
	}
	for _, r := range rows {
		out[r.Day.UTC().Truncate(24*time.Hour)] = r.CommitCount
	}
	return out, nil
}
