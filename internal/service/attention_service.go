package service

import (
	"context"
	"sort"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type AttentionKind string

const (
	AttentionIssueAssigned AttentionKind = "issue_assigned"
	// Reserved for later phases — backing schema does not yet exist.
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
}

// Only surfaces open issues assigned to the user that the user did NOT author.
type AttentionService struct {
	issues *store.IssueStore
}

func NewAttentionService(issues *store.IssueStore) *AttentionService {
	return &AttentionService{issues: issues}
}

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
