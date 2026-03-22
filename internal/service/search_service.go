package service

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

const searchLimit = 20

// SearchResults holds results across all entity types.
type SearchResults struct {
	Repos  []model.Repository
	Issues []model.Issue
	Pulls  []model.PullRequest
	Users  []model.User
	Query  string
	Type   string // "all","repos","issues","pulls","users"
}

type SearchService struct {
	search *store.SearchStore
}

func NewSearchService(search *store.SearchStore) *SearchService {
	return &SearchService{search: search}
}

func (s *SearchService) Search(ctx context.Context, query, searchType string, requestingUserID *int64) (*SearchResults, error) {
	results := &SearchResults{Query: query, Type: searchType}

	g, ctx := errgroup.WithContext(ctx)

	if searchType == "all" || searchType == "repos" || searchType == "" {
		g.Go(func() error {
			repos, err := s.search.SearchRepos(ctx, query, requestingUserID, searchLimit)
			if err != nil {
				return nil // non-fatal: just omit results
			}
			results.Repos = repos
			return nil
		})
	}

	if searchType == "all" || searchType == "issues" || searchType == "" {
		g.Go(func() error {
			issues, err := s.search.SearchIssues(ctx, query, searchLimit)
			if err != nil {
				return nil
			}
			results.Issues = issues
			return nil
		})
	}

	if searchType == "all" || searchType == "pulls" || searchType == "" {
		g.Go(func() error {
			pulls, err := s.search.SearchPulls(ctx, query, searchLimit)
			if err != nil {
				return nil
			}
			results.Pulls = pulls
			return nil
		})
	}

	if searchType == "all" || searchType == "users" || searchType == "" {
		g.Go(func() error {
			users, err := s.search.SearchUsers(ctx, query, searchLimit)
			if err != nil {
				return nil
			}
			results.Users = users
			return nil
		})
	}

	_ = g.Wait()
	return results, nil
}
