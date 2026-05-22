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
	AttentionPRAssigned        AttentionKind = "pr_assigned"
	AttentionPRReviewRequested AttentionKind = "pr_review_requested"
	AttentionMention           AttentionKind = "mention"
)

type AttentionItem struct {
	Kind         AttentionKind
	RefID        int64
	RepoName     string // "owner/name"
	Title        string
	Number       int
	UpdatedAt    time.Time
	WaitingSince time.Time // when this item started needing the user
	URL          string
	Actor        string // author username; empty when lookup fails
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

	appendIssue := func(i store.IssueListItem, kind AttentionKind, url string, waitingSince time.Time) {
		out = append(out, AttentionItem{
			Kind:         kind,
			RefID:        i.ID,
			RepoName:     i.RepoFullName,
			Title:        i.Title,
			Number:       i.Number,
			UpdatedAt:    i.UpdatedAt,
			WaitingSince: waitingSince,
			URL:          url,
		})
		authorIDs = append(authorIDs, i.AuthorID)
	}

	appendPull := func(p store.PullListItem, kind AttentionKind, url string, waitingSince time.Time) {
		out = append(out, AttentionItem{
			Kind:         kind,
			RefID:        p.ID,
			RepoName:     p.RepoFullName,
			Title:        p.Title,
			Number:       p.Number,
			UpdatedAt:    p.UpdatedAt,
			WaitingSince: waitingSince,
			URL:          url,
		})
		authorIDs = append(authorIDs, p.AuthorID)
	}

	// --- assigned issues ---
	issueAssignedAt, err := s.issues.AssignedAtForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	issues, err := s.issues.ListOpenAssignedToUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, i := range issues {
		if i.AuthorID == userID {
			continue
		}
		ws := issueAssignedAt[i.ID]
		appendIssue(i, AttentionIssueAssigned, fmt.Sprintf("/%s/issues/%d", i.RepoFullName, i.Number), ws)
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

	// --- assigned PRs ---
	pullAssignedAt, err := s.pulls.AssignedAtForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	assignedPulls, err := s.pulls.ListForUser(ctx, userID, "assigned", "open")
	if err != nil {
		return nil, err
	}
	for _, p := range assignedPulls {
		ws := pullAssignedAt[p.ID]
		appendPull(p, AttentionPRAssigned, fmt.Sprintf("/%s/pulls/%d", p.RepoFullName, p.Number), ws)
	}

	// --- pending PR reviews ---
	reviewTimes, err := s.pullReview.ListPendingReviewsForReviewer(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(reviewTimes) > 0 {
		reviewPullIDs := make([]int64, 0, len(reviewTimes))
		for id := range reviewTimes {
			reviewPullIDs = append(reviewPullIDs, id)
		}
		prs, err := s.pulls.ListByIDs(ctx, userID, reviewPullIDs, "open")
		if err != nil {
			return nil, err
		}
		for _, p := range prs {
			ws := reviewTimes[p.ID]
			appendPull(p, AttentionPRReviewRequested, fmt.Sprintf("/%s/pulls/%d", p.RepoFullName, p.Number), ws)
		}
	}

	// --- mentions ---
	mentionPullTimes, err := s.mention.ListPullIDsMentioningWithTime(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(mentionPullTimes) > 0 {
		mentionPullIDs := make([]int64, 0, len(mentionPullTimes))
		for id := range mentionPullTimes {
			mentionPullIDs = append(mentionPullIDs, id)
		}
		prs, err := s.pulls.ListByIDs(ctx, userID, mentionPullIDs, "open")
		if err != nil {
			return nil, err
		}
		for _, p := range prs {
			ws := mentionPullTimes[p.ID]
			appendPull(p, AttentionMention, fmt.Sprintf("/%s/pulls/%d", p.RepoFullName, p.Number), ws)
		}
	}

	mentionIssueTimes, err := s.mention.ListIssueIDsMentioningWithTime(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(mentionIssueTimes) > 0 {
		mentionIssueIDs := make([]int64, 0, len(mentionIssueTimes))
		for id := range mentionIssueTimes {
			mentionIssueIDs = append(mentionIssueIDs, id)
		}
		is, err := s.issues.ListByIDs(ctx, userID, mentionIssueIDs, "open")
		if err != nil {
			return nil, err
		}
		for _, i := range is {
			ws := mentionIssueTimes[i.ID]
			appendIssue(i, AttentionMention, fmt.Sprintf("/%s/issues/%d", i.RepoFullName, i.Number), ws)
		}
	}

	out, authorIDs = sortAttentionPaired(out, authorIDs)
	s.resolveActors(ctx, out, authorIDs)
	return out, nil
}

// CountForUser returns the total number of open attention items for userID:
// issues and PRs assigned to or mentioning them, plus PRs awaiting their review.
// The issue and PR counts each resolve in a single folded query.
func (s *AttentionService) CountForUser(ctx context.Context, userID int64) (int, error) {
	ic, err := s.issues.CountsForUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	total := ic["assigned:open"] + ic["mentioned:open"]

	if s.pulls == nil || s.pullReview == nil {
		return total, nil
	}

	pc, err := s.pulls.CountsForUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	total += pc["assigned:open"] + pc["mentioned:open"]

	rn, err := s.pullReview.CountPendingForReviewer(ctx, userID)
	if err != nil {
		return 0, err
	}
	total += rn

	return total, nil
}

// sortAttentionPaired sorts both slices together by WaitingSince ascending
// (oldest waiting = most overdue first), keeping authorIDs[i] as the author of
// out[i] after the sort.
func sortAttentionPaired(items []AttentionItem, authorIDs []int64) ([]AttentionItem, []int64) {
	type pair struct {
		item     AttentionItem
		authorID int64
	}
	pairs := make([]pair, len(items))
	for i := range items {
		pairs[i] = pair{items[i], authorIDs[i]}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].item.WaitingSince.Before(pairs[j].item.WaitingSince)
	})
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
