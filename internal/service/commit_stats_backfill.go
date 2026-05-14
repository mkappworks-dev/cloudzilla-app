package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// BackfillRecentCommits walks the default branch of each repository for the
// last `days` days and ingests per-day commit counts. Safe to re-run: repos
// whose `commit_day_counts` table already has rows in the lookback window are
// skipped, so additive live-push counts (including feature-branch work) are
// not overwritten on subsequent server restarts.
//
// Per-repo errors are logged and swallowed; a final summary log line reports
// the count of skipped/ingested/failed repos.
func (s *CommitStatsService) BackfillRecentCommits(ctx context.Context, repos []model.Repository, code *CodeService, days int) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	var ingested, skipped, failed int
	cancelled := false
	defer func() {
		slog.Info("backfill: complete",
			"ingested", ingested, "skipped", skipped, "failed", failed,
			"total", len(repos), "cancelled", cancelled)
	}()
	for _, r := range repos {
		if ctx.Err() != nil {
			cancelled = true
			return ctx.Err()
		}
		has, err := s.stats.HasRowsForRepoSince(ctx, r.ID, cutoff)
		if err != nil {
			failed++
			slog.Warn("backfill: existence check failed", "repo_id", r.ID, "owner", r.OwnerName, "name", r.Name, "error", err)
			continue
		}
		if has {
			skipped++
			continue
		}
		ref := r.DefaultBranch
		if ref == "" {
			ref = "HEAD"
		}
		commits, err := code.LogSince(ctx, r.OwnerName, r.Name, ref, cutoff)
		if err != nil {
			failed++
			slog.Warn("backfill: log failed", "repo_id", r.ID, "owner", r.OwnerName, "name", r.Name, "error", err)
			continue
		}
		if len(commits) == 0 {
			continue
		}
		samples := make([]CommitSample, 0, len(commits))
		for _, c := range commits {
			samples = append(samples, CommitSample{AuthorEmail: c.AuthorEmail, Time: c.Time})
		}
		if err := s.Ingest(ctx, r.ID, samples); err != nil {
			failed++
			slog.Warn("backfill: ingest failed", "repo_id", r.ID, "error", err)
			continue
		}
		ingested++
	}
	return nil
}
