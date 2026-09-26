package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var ErrInvitationUnusable = store.ErrInvitationUnusable

// InvitationService manages invite tokens for user registration.
type InvitationService struct {
	store *store.InvitationStore
}

// NewInvitationService creates an InvitationService backed by the given store.
func NewInvitationService(s *store.InvitationStore) *InvitationService {
	return &InvitationService{store: s}
}

func (s *InvitationService) Create(ctx context.Context, invitedByID int64, email string) (*model.Invitation, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	inv := &model.Invitation{
		Token:       hex.EncodeToString(b),
		Email:       email,
		InvitedByID: invitedByID,
		ExpiresAt:   time.Now().UTC().Add(7 * 24 * time.Hour),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.Create(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// GetUsable returns ErrInvitationUnusable alike for unknown, accepted, expired,
// and already-registered invitations so the page can't tell them apart.
func (s *InvitationService) GetUsable(ctx context.Context, token string) (*model.Invitation, error) {
	return s.store.GetUsableByToken(ctx, token)
}

func (s *InvitationService) List(ctx context.Context) ([]model.Invitation, error) {
	return s.store.ListAll(ctx)
}

func (s *InvitationService) Delete(ctx context.Context, id int64) error {
	return s.store.Delete(ctx, id)
}
