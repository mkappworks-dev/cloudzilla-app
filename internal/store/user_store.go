package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	storedb "github.com/mkappworks/cloudzilla/internal/store/db"
)

type UserStore struct {
	q  *storedb.Queries
	db *sql.DB
}

func NewUserStore(q *storedb.Queries, database *sql.DB) *UserStore {
	return &UserStore{q: q, db: database}
}

func (s *UserStore) Create(ctx context.Context, u *model.User) error {
	result, err := s.q.CreateUser(ctx, storedb.CreateUserParams{
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

// GetByEmailWithRole fetches a user by email including is_superadmin and is_invited columns
// added via ALTER TABLE (not known to sqlc).
func (s *UserStore) GetByEmailWithRole(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id,
		        is_superadmin, is_invited, created_at, updated_at
		 FROM users WHERE email = ?`,
		email,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Bio, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.IsSuperadmin, &u.IsInvited,
		&u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user get by email with role: %w", err)
	}
	return u, nil
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
	result, err := s.q.CreateOAuthUser(ctx, storedb.CreateOAuthUserParams{
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

func (s *UserStore) CountAll(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("user count all: %w", err)
	}
	return count, nil
}

func (s *UserStore) CreateSuperadmin(ctx context.Context, username, email, passwordHash string) (*model.User, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash, bio, avatar_url, is_superadmin, created_at, updated_at)
		 VALUES (?, ?, ?, '', '', TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		username, email, passwordHash,
	)
	if err != nil {
		return nil, fmt.Errorf("create superadmin: %w", err)
	}
	return s.GetByEmailWithRole(ctx, email)
}

func (s *UserStore) MarkInvited(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET is_invited = TRUE WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("user mark invited: %w", err)
	}
	return nil
}

func mapDBUserToModel(dbUser *storedb.User) *model.User {
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
