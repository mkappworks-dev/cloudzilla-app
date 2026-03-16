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

func mapDBUserToModel(dbUser *db.User) *model.User {
	return &model.User{
		ID:           dbUser.ID,
		Username:     dbUser.Username,
		Email:        dbUser.Email,
		PasswordHash: dbUser.PasswordHash,
		Bio:          dbUser.Bio,
		AvatarURL:    dbUser.AvatarUrl,
		CreatedAt:    dbUser.CreatedAt,
		UpdatedAt:    dbUser.UpdatedAt,
	}
}
