package service

import (
	"context"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
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

func (s *ReactionService) ToggleDiscussion(ctx context.Context, userID, discussionID int64, emoji string) (bool, error) {
	if !validEmoji(emoji) {
		return false, fmt.Errorf("unsupported emoji: %s", emoji)
	}
	return s.store.ToggleDiscussion(ctx, userID, discussionID, emoji)
}

func (s *ReactionService) ListByDiscussion(ctx context.Context, discussionID, callerID int64) ([]model.ReactionSummary, error) {
	reactions, err := s.store.ListByDiscussion(ctx, discussionID, callerID)
	if reactions == nil {
		reactions = []model.ReactionSummary{}
	}
	return reactions, err
}

func (s *ReactionService) ToggleReply(ctx context.Context, userID, replyID int64, emoji string) (bool, error) {
	if !validEmoji(emoji) {
		return false, fmt.Errorf("unsupported emoji: %s", emoji)
	}
	return s.store.ToggleReply(ctx, userID, replyID, emoji)
}

func (s *ReactionService) ListByReply(ctx context.Context, replyID, callerID int64) ([]model.ReactionSummary, error) {
	reactions, err := s.store.ListByReply(ctx, replyID, callerID)
	if reactions == nil {
		reactions = []model.ReactionSummary{}
	}
	return reactions, err
}
