package service

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

var allowedEmojis = map[string]bool{
	"+1": true, "-1": true, "laugh": true, "hooray": true,
	"confused": true, "heart": true, "rocket": true, "eyes": true,
}

func validEmoji(emoji string) bool { return allowedEmojis[emoji] }

// ReactionService manages emoji reactions on issue and PR comments.
type ReactionService struct{ store *store.ReactionStore }

// NewReactionService creates a ReactionService backed by the given reaction store.
func NewReactionService(s *store.ReactionStore) *ReactionService {
	return &ReactionService{store: s}
}

// Toggle adds or removes a reaction. Returns (true, nil) if added, (false, nil) if removed.
func (s *ReactionService) Toggle(ctx context.Context, userID, commentID int64, emoji string) (bool, error) {
	if !validEmoji(emoji) {
		return false, fmt.Errorf("unsupported emoji: %s", emoji)
	}
	return s.store.Toggle(ctx, userID, commentID, emoji)
}

// CommentBelongsToRepo returns true if the comment belongs to the given repo.
func (s *ReactionService) CommentBelongsToRepo(ctx context.Context, commentID, repoID int64) (bool, error) {
	return s.store.CommentBelongsToRepo(ctx, commentID, repoID)
}

// List returns reaction summaries for one comment. callerID=0 means anonymous.
func (s *ReactionService) List(ctx context.Context, commentID, callerID int64) ([]model.ReactionSummary, error) {
	reactions, err := s.store.ListByComment(ctx, commentID, callerID)
	if reactions == nil {
		reactions = []model.ReactionSummary{}
	}
	return reactions, err
}
