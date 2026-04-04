package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// savedReplyValidate is a pure validation helper (also tested in unit tests).
func savedReplyValidate(title, body string) string {
	if strings.TrimSpace(title) == "" {
		return "title is required"
	}
	if strings.TrimSpace(body) == "" {
		return "body is required"
	}
	return ""
}

type SavedReplyService struct {
	replies *store.SavedReplyStore
}

func NewSavedReplyService(replies *store.SavedReplyStore) *SavedReplyService {
	return &SavedReplyService{replies: replies}
}

func (s *SavedReplyService) Create(ctx context.Context, userID int64, title, body string) (*model.SavedReply, error) {
	if msg := savedReplyValidate(title, body); msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}
	r := &model.SavedReply{
		UserID: userID,
		Title:  strings.TrimSpace(title),
		Body:   strings.TrimSpace(body),
	}
	if err := s.replies.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *SavedReplyService) List(ctx context.Context, userID int64) ([]model.SavedReply, error) {
	replies, err := s.replies.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if replies == nil {
		replies = []model.SavedReply{}
	}
	return replies, nil
}

func (s *SavedReplyService) Update(ctx context.Context, id, userID int64, title, body string) (*model.SavedReply, error) {
	if msg := savedReplyValidate(title, body); msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}
	existing, err := s.replies.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("saved reply not found: %w", err)
	}
	if existing.UserID != userID {
		return nil, fmt.Errorf("forbidden")
	}
	existing.Title = strings.TrimSpace(title)
	existing.Body = strings.TrimSpace(body)
	if err := s.replies.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *SavedReplyService) Delete(ctx context.Context, id, userID int64) error {
	return s.replies.Delete(ctx, id, userID)
}
