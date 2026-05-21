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
}

// AttentionService surfaces open items that need the user's attention across
// assigned issues, pending PR reviews, and mentions.
type AttentionService struct {
	issues     *store.IssueStore
	pulls      *store.PullStore
	pullReview *store.PullReviewStore
	mention    *store.MentionStore
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

func (s *AttentionService) ForUser(ctx context.Context, userID int64) ([]AttentionItem, error) {
	var out []AttentionItem

	// --- assigned issues ---
	issues, err := s.issues.ListOpenAssignedToUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, i := range issues {
		if i.AuthorID == userID {
			continue
		}
		out = append(out, AttentionItem{
			Kind:      AttentionIssueAssigned,
			RefID:     i.ID,
			RepoName:  i.RepoFullName,
			Title:     i.Title,
			Number:    i.Number,
			UpdatedAt: i.UpdatedAt,
			URL:       fmt.Sprintf("/%s/issues/%d", i.RepoFullName, i.Number),
		})
	}

	if s.pulls == nil || s.pullReview == nil || s.mention == nil {
		sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
		if len(out) > 20 {
			out = out[:20]
		}
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
			out = append(out, AttentionItem{
				Kind:      AttentionPRReviewRequested,
				RefID:     p.ID,
				RepoName:  p.RepoFullName,
				Title:     p.Title,
				Number:    p.Number,
				UpdatedAt: p.UpdatedAt,
				URL:       fmt.Sprintf("/%s/pulls/%d", p.RepoFullName, p.Number),
			})
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
			out = append(out, AttentionItem{
				Kind:      AttentionMention,
				RefID:     p.ID,
				RepoName:  p.RepoFullName,
				Title:     p.Title,
				Number:    p.Number,
				UpdatedAt: p.UpdatedAt,
				URL:       fmt.Sprintf("/%s/pulls/%d", p.RepoFullName, p.Number),
			})
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
			out = append(out, AttentionItem{
				Kind:      AttentionMention,
				RefID:     i.ID,
				RepoName:  i.RepoFullName,
				Title:     i.Title,
				Number:    i.Number,
				UpdatedAt: i.UpdatedAt,
				URL:       fmt.Sprintf("/%s/issues/%d", i.RepoFullName, i.Number),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
