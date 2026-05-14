package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// Safe to re-run: UpsertCount is idempotent on (repo, user, day). Per-repo errors are logged and swallowed.
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
