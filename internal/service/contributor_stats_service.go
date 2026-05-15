package service

import (
	"context"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type ContributorStatsService struct {
	stats *store.ContributorStatsStore
	users *store.UserStore
}

func NewContributorStatsService(stats *store.ContributorStatsStore, users *store.UserStore) *ContributorStatsService {
	return &ContributorStatsService{stats: stats, users: users}
}

type ContributorWithTimeline struct {
	Username  string
	Commits   int
	Additions int
	Deletions int
	Timeline  []int // weekly commit counts oldest→newest
}

func (s *ContributorStatsService) ForRepo(ctx context.Context, repoID int64) ([]ContributorWithTimeline, error) {
	rows, err := s.stats.ListForRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	type bucket struct {
		commits, additions, deletions int
		timeline                      map[time.Time]int
	}
	groups := make(map[int64]*bucket)
	names := make(map[int64]string)
	var minWeek, maxWeek time.Time
	for _, r := range rows {
		g, ok := groups[r.UserID]
		if !ok {
			g = &bucket{timeline: map[time.Time]int{}}
			groups[r.UserID] = g
			names[r.UserID] = r.Username
		}
		g.commits += r.Commits
		g.additions += r.Additions
		g.deletions += r.Deletions
		g.timeline[r.Week] = r.Commits
		if minWeek.IsZero() || r.Week.Before(minWeek) {
			minWeek = r.Week
		}
		if r.Week.After(maxWeek) {
			maxWeek = r.Week
		}
	}
	var out []ContributorWithTimeline
	for uid, g := range groups {
		var tl []int
		if !minWeek.IsZero() {
			for w := minWeek; !w.After(maxWeek); w = w.AddDate(0, 0, 7) {
				tl = append(tl, g.timeline[w])
			}
		}
		out = append(out, ContributorWithTimeline{
			Username:  names[uid],
			Commits:   g.commits,
			Additions: g.additions,
			Deletions: g.deletions,
			Timeline:  tl,
		})
	}
	// Sort by commit count descending (insertion sort — contributor lists are short).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Commits < out[j].Commits; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

// IngestCommit adds one commit's diff stats to the per-week aggregate for (repoID, userID).
// It reads the current bucket first so successive calls within the same week accumulate correctly.
func (s *ContributorStatsService) IngestCommit(ctx context.Context, repoID, userID int64, when time.Time, additions, deletions int) error {
	week := store.MondayUTC(when)
	existing, err := s.stats.GetOne(ctx, repoID, userID, week)
	if err != nil {
		return err
	}
	return s.stats.UpsertStats(ctx, repoID, userID, week,
		existing.Commits+1, existing.Additions+additions, existing.Deletions+deletions)
}
