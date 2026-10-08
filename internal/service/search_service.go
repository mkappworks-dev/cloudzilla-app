package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const searchLimit = 20

// SearchResults holds results across all entity types.
// SearchResults holds paginated results across repos, issues, PRs, and users.
type SearchResults struct {
	Repos  []model.Repository
	Issues []model.Issue
	Pulls  []model.PullRequest
	Users  []model.User
	Query  string
	Type   string // "all","repos","issues","pulls","users"
}

// SearchService provides full-text search across repositories, issues, PRs, and users.
type SearchService struct {
	search *store.SearchStore
}

// NewSearchService creates a SearchService backed by the given search store.
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
			issues, err := s.search.SearchIssues(ctx, query, requestingUserID, searchLimit)
			if err != nil {
				return nil
			}
			results.Issues = issues
			return nil
		})
	}

	if searchType == "all" || searchType == "pulls" || searchType == "" {
		g.Go(func() error {
			pulls, err := s.search.SearchPulls(ctx, query, requestingUserID, searchLimit)
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

const (
	suggestMinQuery  = 2
	suggestMaxQuery  = 100
	suggestRepoLimit = 4
	suggestUserLimit = 3
	suggestOrgLimit  = 3
)

// Suggestions are the few top matches shown under the topnav search field.
type Suggestions struct {
	Repos []model.Repository
	Users []model.User
	Orgs  []model.Organization
	Query string // trimmed and capped; what the "Search for" row links to
}

// Suggest returns nothing for a query under suggestMinQuery characters, so a
// single keystroke never scans the tables.
func (s *SearchService) Suggest(ctx context.Context, query string, requestingUserID *int64) (*Suggestions, error) {
	query = strings.TrimSpace(query)
	if r := []rune(query); len(r) > suggestMaxQuery {
		query = string(r[:suggestMaxQuery])
	}
	out := &Suggestions{Query: query}
	if utf8.RuneCountInString(query) < suggestMinQuery {
		return out, nil
	}

	var g errgroup.Group
	g.Go(func() error {
		if repos, err := s.search.SuggestRepos(ctx, query, requestingUserID, suggestRepoLimit); err == nil {
			out.Repos = repos
		}
		return nil
	})
	g.Go(func() error {
		if users, err := s.search.SuggestUsers(ctx, query, suggestUserLimit); err == nil {
			out.Users = users
		}
		return nil
	})
	g.Go(func() error {
		if orgs, err := s.search.SuggestOrgs(ctx, query, suggestOrgLimit); err == nil {
			out.Orgs = orgs
		}
		return nil
	})
	_ = g.Wait()
	return out, nil
}
