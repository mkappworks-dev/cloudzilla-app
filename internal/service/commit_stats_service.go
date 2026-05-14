package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type CommitSample struct {
	AuthorEmail string
	Time        time.Time
}

type CommitStatsService struct {
	stats *store.CommitStatsStore
	users *store.UserStore
}

func NewCommitStatsService(stats *store.CommitStatsStore, users *store.UserStore) *CommitStatsService {
	return &CommitStatsService{stats: stats, users: users}
}

// Uses AddCount (additive) so successive pushes within the same day accumulate. Commits whose author email matches no user are skipped.
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
	var firstErr error
	failed := 0
	for k, count := range buckets {
		if err := s.stats.AddCount(ctx, repoID, k.userID, k.day, count); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("commit stats: add count failed",
				"repo_id", repoID, "user_id", k.userID, "day", k.day, "delta", count, "error", err)
		}
	}
	if firstErr != nil {
		slog.Warn("commit stats: bucket aggregate failure",
			"repo_id", repoID, "failed", failed, "total", len(buckets))
		return fmt.Errorf("commit stats: %d/%d buckets failed: %w", failed, len(buckets), firstErr)
	}
	return nil
}

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

// Materializes the full window with explicit zero entries so the heatmap can render a regular grid.
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

// Materializes the full window with explicit zero entries so the heatmap can render a regular grid.
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
