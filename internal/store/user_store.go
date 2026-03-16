package store

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type UserStore struct{ q *db.Queries }

func NewUserStore(q *db.Queries) *UserStore { return &UserStore{q: q} }

func (s *UserStore) Create(ctx context.Context, u *model.User) error {
	result, err := s.q.CreateUser(ctx, db.CreateUserParams{
		Username:     u.Username,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Bio:          u.Bio,
		AvatarUrl:    u.AvatarURL,
	})
	if err != nil {
		return fmt.Errorf("user create: %w", err)
	}
	u.ID = result.ID
	u.CreatedAt = result.CreatedAt
	u.UpdatedAt = result.UpdatedAt
	return nil
}

func (s *UserStore) GetByID(ctx context.Context, id int64) (*model.User, error) {
	result, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("user get by id: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	result, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("user get by username: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	result, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("user get by email: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func (s *UserStore) GetByOAuthID(ctx context.Context, provider, oauthID string) (*model.User, error) {
	result, err := s.q.GetUserByOAuthID(ctx, provider, oauthID)
	if err != nil {
		return nil, fmt.Errorf("user get by oauth id: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func (s *UserStore) LinkOAuth(ctx context.Context, userID int64, provider, oauthID string) error {
	if err := s.q.LinkOAuth(ctx, userID, provider, oauthID); err != nil {
		return fmt.Errorf("user link oauth: %w", err)
	}
	return nil
}

func (s *UserStore) CreateOAuthUser(ctx context.Context, username, email, provider, oauthID, avatarURL string) (*model.User, error) {
	result, err := s.q.CreateOAuthUser(ctx, db.CreateOAuthUserParams{
		Username:      username,
		Email:         email,
		OAuthProvider: provider,
		OAuthID:       oauthID,
		AvatarURL:     avatarURL,
	})
	if err != nil {
		return nil, fmt.Errorf("user create oauth: %w", err)
	}
	return mapDBUserToModel(&result), nil
}

func mapDBUserToModel(dbUser *db.User) *model.User {
	return &model.User{
		ID:            dbUser.ID,
		Username:      dbUser.Username,
		Email:         dbUser.Email,
		PasswordHash:  dbUser.PasswordHash,
		Bio:           dbUser.Bio,
		AvatarURL:     dbUser.AvatarUrl,
		OAuthProvider: dbUser.OAuthProvider,
		OAuthID:       dbUser.OAuthID,
		CreatedAt:     dbUser.CreatedAt,
		UpdatedAt:     dbUser.UpdatedAt,
	}
}
