package service

import (
	"context"
	"log/slog"
	"slices"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// RecordMergedRange stores the tips a merge saw, since afterwards the live branches no longer show what it merged.
func (s *PullService) RecordMergedRange(ctx context.Context, pullID int64, baseSHA, headSHA string) error {
	return s.pulls.SetMergedRange(ctx, pullID, baseSHA, headSHA)
}

// BackfillMergedRanges rebuilds the range of PRs merged before it was recorded. One that can't be rebuilt is stored
// empty, so it isn't retried on every start and its pages say the diff is unavailable.
func (s *PullService) BackfillMergedRanges(ctx context.Context, events *PullEventService) {
	pulls, err := s.pulls.MergedWithoutRange(ctx)
	if err != nil {
		slog.Warn("backfill merged pull ranges: list failed", "error", err)
		return
	}
	rebuilt := 0
	for _, pr := range pulls {
		if ctx.Err() != nil {
			return
		}
		commits := 0
		if evs, err := events.ListByPull(ctx, pr.ID); err == nil {
			for _, ev := range slices.Backward(evs) {
				if ev.Type == model.PullEventMerged {
					commits, _ = strconv.Atoi(ev.Detail)
					break
				}
			}
		}
		base, head, ok := s.code.ReconstructMergedRange(pr.RepoOwner, pr.RepoName, pr.BaseBranch, pr.HeadBranch, commits)
		if ok {
			rebuilt++
		}
		if err := s.pulls.SetMergedRange(ctx, pr.ID, base, head); err != nil {
			slog.Warn("backfill merged pull ranges: save failed", "pull_id", pr.ID, "error", err)
		}
	}
	if len(pulls) > 0 {
		slog.Info("backfill merged pull ranges", "merged_without_range", len(pulls), "rebuilt", rebuilt)
	}
}
