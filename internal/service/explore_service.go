package service

import (
	"context"
	"strings"
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

// ExplorePageSize is how many repositories one Explore page lists.
const ExplorePageSize = exploreLimit

// ExploreQuery selects an Explore listing. Tab is "trending", "newest" or "forked"; Page counts from 1.
type ExploreQuery struct {
	Tab      string
	Period   string
	Text     string
	Language string
	Page     int
}

// Repos returns one page of public repos for q and the total number that match.
func (s *ExploreService) Repos(ctx context.Context, q ExploreQuery) ([]model.RepositoryWithStats, int, error) {
	order := store.ExploreByTrending
	switch q.Tab {
	case "newest":
		order = store.ExploreByNewest
	case "forked":
		order = store.ExploreByForks
	}
	page := max(q.Page, 1)
	return s.explore.ListPublicRepos(ctx, order, time.Now().Add(-periodToDuration(q.Period)), store.ExploreFilter{
		Text:     strings.TrimSpace(q.Text),
		Language: q.Language,
		Limit:    ExplorePageSize,
		Offset:   (page - 1) * ExplorePageSize,
	})
}

// Languages lists the languages Explore can filter by.
func (s *ExploreService) Languages(ctx context.Context) ([]string, error) {
	return s.explore.PublicLanguages(ctx)
}
