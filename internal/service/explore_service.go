package service

import (
	"context"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const exploreLimit = 25

// periodToDuration converts a period name to a lookback duration.
// Defaults to weekly for unknown or empty values.
func periodToDuration(period string) time.Duration {
	switch period {
	case "daily":
		return 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

// ExploreService provides trending repository and user discovery.
type ExploreService struct {
	explore *store.ExploreStore
}

// NewExploreService creates an ExploreService backed by the given explore store.
func NewExploreService(explore *store.ExploreStore) *ExploreService {
	return &ExploreService{explore: explore}
}

// Trending returns the most-starred public repos in the given time window.
func (s *ExploreService) Trending(ctx context.Context, period string) ([]model.RepositoryWithStats, error) {
	since := time.Now().Add(-periodToDuration(period))
	repos, err := s.explore.TrendingRepos(ctx, since, exploreLimit)
	if err != nil {
		return nil, err
	}
	return repos, nil
}

// Newest returns the most recently created public repos.
func (s *ExploreService) Newest(ctx context.Context) ([]model.RepositoryWithStats, error) {
	repos, err := s.explore.NewestRepos(ctx, exploreLimit)
	if err != nil {
		return nil, err
	}
	return repos, nil
}

// MostForked returns public repos with the highest fork count.
func (s *ExploreService) MostForked(ctx context.Context) ([]model.RepositoryWithStats, error) {
	repos, err := s.explore.MostForkedRepos(ctx, exploreLimit)
	if err != nil {
		return nil, err
	}
	return repos, nil
}
