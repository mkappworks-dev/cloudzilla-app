package db

import (
	"context"
)

const getUserByOAuthID = `
SELECT id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id, created_at, updated_at
FROM users WHERE oauth_provider = $1 AND oauth_id = $2 LIMIT 1
`

func (q *Queries) GetUserByOAuthID(ctx context.Context, provider, oauthID string) (User, error) {
	row := q.db.QueryRowContext(ctx, getUserByOAuthID, provider, oauthID)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Username,
		&i.Email,
		&i.PasswordHash,
		&i.Bio,
		&i.AvatarUrl,
		&i.OauthProvider,
		&i.OauthID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const linkOAuth = `
UPDATE users SET oauth_provider = $1, oauth_id = $2, updated_at = NOW()
WHERE id = $3
`

func (q *Queries) LinkOAuth(ctx context.Context, userID int64, provider, oauthID string) error {
	_, err := q.db.ExecContext(ctx, linkOAuth, provider, oauthID, userID)
	return err
}

const createOAuthUser = `
INSERT INTO users (username, email, password_hash, oauth_provider, oauth_id, avatar_url)
VALUES ($1, $2, '', $3, $4, $5)
RETURNING id, username, email, password_hash, bio, avatar_url, oauth_provider, oauth_id, created_at, updated_at
`

type CreateOAuthUserParams struct {
	Username      string
	Email         string
	OAuthProvider string
	OAuthID       string
	AvatarURL     string
}

func (q *Queries) CreateOAuthUser(ctx context.Context, arg CreateOAuthUserParams) (User, error) {
	row := q.db.QueryRowContext(ctx, createOAuthUser,
		arg.Username,
		arg.Email,
		arg.OAuthProvider,
		arg.OAuthID,
		arg.AvatarURL,
	)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Username,
		&i.Email,
		&i.PasswordHash,
		&i.Bio,
		&i.AvatarUrl,
		&i.OauthProvider,
		&i.OauthID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}
