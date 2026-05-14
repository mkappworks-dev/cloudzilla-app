package service

import (
	"context"
	"sort"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// AttentionKind labels each row of the home page attention list so the
// UI can pick the right icon and verb.
type AttentionKind string

const (
	AttentionIssueAssigned AttentionKind = "issue_assigned"
	// Reserved for later phases — schema (pull_review_requests table,
	// read-state on mentions) does not exist in Phase 1.
	AttentionPRReviewRequested AttentionKind = "pr_review_requested"
	AttentionMention           AttentionKind = "mention"
)

// AttentionItem is one row in the home page attention list.
type AttentionItem struct {
	Kind      AttentionKind
	RefID     int64  // issue or PR ID
	RepoName  string // "owner/name"
	Title     string
	Number    int // issue/PR number for display
	UpdatedAt time.Time
}

// AttentionService backs the home page "what needs your attention" list.
// Phase 1 only surfaces open issues assigned to the user that the user did
// NOT author. PR review-request and mention paths are deferred to a later
// phase because the underlying schema does not yet exist.
type AttentionService struct {
	issues *store.IssueStore
}

// NewAttentionService constructs an AttentionService.
func NewAttentionService(issues *store.IssueStore) *AttentionService {
	return &AttentionService{issues: issues}
}

// ForUser returns the home page attention list, capped at 20 items,
// sorted by UpdatedAt descending. Phase 1 only surfaces open issues
// assigned to the user that the user did NOT author. PR review-request
// and mention paths are deferred to a later phase.
func (s *AttentionService) ForUser(ctx context.Context, userID int64) ([]AttentionItem, error) {
	issues, err := s.issues.ListOpenAssignedToUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]AttentionItem, 0, len(issues))
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
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out, nil
}
