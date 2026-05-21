package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type AttentionKind string

const (
	AttentionIssueAssigned     AttentionKind = "issue_assigned"
	AttentionPRReviewRequested AttentionKind = "pr_review_requested"
	AttentionMention           AttentionKind = "mention"
)

type AttentionItem struct {
	Kind      AttentionKind
	RefID     int64
	RepoName  string // "owner/name"
	Title     string
	Number    int
	UpdatedAt time.Time
	URL       string
	Actor     string // author username; empty when lookup fails
}

// AttentionService surfaces open items that need the user's attention across
// assigned issues, pending PR reviews, and mentions.
type AttentionService struct {
	issues     *store.IssueStore
	pulls      *store.PullStore
	pullReview *store.PullReviewStore
	mention    *store.MentionStore
	users      *store.UserStore
}

func NewAttentionService(issues *store.IssueStore) *AttentionService {
	return &AttentionService{issues: issues}
}

func (s *AttentionService) WithPullDeps(pulls *store.PullStore, pullReview *store.PullReviewStore, mention *store.MentionStore) *AttentionService {
	s.pulls = pulls
	s.pullReview = pullReview
	s.mention = mention
	return s
}

func (s *AttentionService) WithUserStore(users *store.UserStore) *AttentionService {
	s.users = users
	return s
}

func (s *AttentionService) ForUser(ctx context.Context, userID int64) ([]AttentionItem, error) {
	var out []AttentionItem
	var authorIDs []int64 // parallel to out; authorIDs[i] is the author of out[i]

	appendIssue := func(i store.IssueListItem, kind AttentionKind, url string) {
		out = append(out, AttentionItem{
			Kind:      kind,
			RefID:     i.ID,
			RepoName:  i.RepoFullName,
			Title:     i.Title,
			Number:    i.Number,
			UpdatedAt: i.UpdatedAt,
			URL:       url,
		})
		authorIDs = append(authorIDs, i.AuthorID)
	}

	appendPull := func(p store.PullListItem, kind AttentionKind, url string) {
		out = append(out, AttentionItem{
			Kind:      kind,
			RefID:     p.ID,
			RepoName:  p.RepoFullName,
			Title:     p.Title,
			Number:    p.Number,
			UpdatedAt: p.UpdatedAt,
			URL:       url,
		})
		authorIDs = append(authorIDs, p.AuthorID)
	}

	// --- assigned issues ---
	issues, err := s.issues.ListOpenAssignedToUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, i := range issues {
		if i.AuthorID == userID {
			continue
		}
		appendIssue(i, AttentionIssueAssigned, fmt.Sprintf("/%s/issues/%d", i.RepoFullName, i.Number))
	}

	if s.pulls == nil || s.pullReview == nil || s.mention == nil {
		out, authorIDs = sortAttentionPaired(out, authorIDs)
		if len(out) > 20 {
			out = out[:20]
			authorIDs = authorIDs[:20]
		}
		s.resolveActors(ctx, out, authorIDs)
		return out, nil
	}

	// --- pending PR reviews ---
	reviewPullIDs, err := s.pullReview.ListPullIDsAwaitingReviewer(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(reviewPullIDs) > 0 {
		prs, err := s.pulls.ListByIDs(ctx, userID, reviewPullIDs, "open")
		if err != nil {
			return nil, err
		}
		for _, p := range prs {
			appendPull(p, AttentionPRReviewRequested, fmt.Sprintf("/%s/pulls/%d", p.RepoFullName, p.Number))
		}
	}

	// --- mentions ---
	mentionPullIDs, err := s.mention.ListPullIDsMentioning(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(mentionPullIDs) > 0 {
		prs, err := s.pulls.ListByIDs(ctx, userID, mentionPullIDs, "open")
		if err != nil {
			return nil, err
		}
		for _, p := range prs {
			appendPull(p, AttentionMention, fmt.Sprintf("/%s/pulls/%d", p.RepoFullName, p.Number))
		}
	}

	mentionIssueIDs, err := s.mention.ListIssueIDsMentioning(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(mentionIssueIDs) > 0 {
		issues, err := s.issues.ListByIDs(ctx, userID, mentionIssueIDs, "open")
		if err != nil {
			return nil, err
		}
		for _, i := range issues {
			appendIssue(i, AttentionMention, fmt.Sprintf("/%s/issues/%d", i.RepoFullName, i.Number))
		}
	}

	out, authorIDs = sortAttentionPaired(out, authorIDs)
	s.resolveActors(ctx, out, authorIDs)
	return out, nil
}

// sortAttentionPaired sorts both slices together by UpdatedAt descending,
// keeping authorIDs[i] as the author of out[i] after the sort.
func sortAttentionPaired(items []AttentionItem, authorIDs []int64) ([]AttentionItem, []int64) {
	type pair struct {
		item     AttentionItem
		authorID int64
	}
	pairs := make([]pair, len(items))
	for i := range items {
		pairs[i] = pair{items[i], authorIDs[i]}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].item.UpdatedAt.After(pairs[j].item.UpdatedAt) })
	for i := range pairs {
		items[i] = pairs[i].item
		authorIDs[i] = pairs[i].authorID
	}
	return items, authorIDs
}

// resolveActors does a single batch lookup and sets Actor on each item.
// authorIDs[i] is the author ID for items[i].
func (s *AttentionService) resolveActors(ctx context.Context, items []AttentionItem, authorIDs []int64) {
	if s.users == nil || len(items) == 0 {
		return
	}
	seen := make(map[int64]struct{}, len(authorIDs))
	for _, id := range authorIDs {
		seen[id] = struct{}{}
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	usernames, err := s.users.UsernamesByIDs(ctx, ids)
	if err != nil {
		return
	}
	for i := range items {
		if i < len(authorIDs) {
			if name, ok := usernames[authorIDs[i]]; ok {
				items[i].Actor = name
			}
		}
	}
}
