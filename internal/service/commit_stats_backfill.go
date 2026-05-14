package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// BackfillRecentCommits walks each repo's default branch over the last `days`
// days and re-ingests commits into commit_day_counts. It is safe to re-run
// because CommitStatsStore.UpsertCount is idempotent on (repo, user, day).
//
// Errors on a single repo are logged and swallowed so one bad repo does not
// halt the whole backfill. The walk honors ctx.Err() so a deadline cancels
// the job cleanly.
func (s *CommitStatsService) BackfillRecentCommits(ctx context.Context, repos []model.Repository, code *CodeService, days int) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	for _, r := range repos {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ref := r.DefaultBranch
		if ref == "" {
			ref = "HEAD"
		}
		commits, err := code.LogSince(ctx, r.OwnerName, r.Name, ref, cutoff)
		if err != nil {
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
			slog.Warn("backfill: ingest failed", "repo_id", r.ID, "error", err)
		}
	}
	return nil
}
