package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type AttachmentStore struct{ db *sql.DB }

func NewAttachmentStore(db *sql.DB) *AttachmentStore { return &AttachmentStore{db: db} }

const attachmentColumns = `token, repo_id, uploader_id, storage_key, content_type, size_bytes, created_at`

func scanAttachment(row interface{ Scan(...any) error }) (*model.Attachment, error) {
	var a model.Attachment
	var uploader sql.NullInt64
	if err := row.Scan(&a.Token, &a.RepoID, &uploader, &a.StorageKey, &a.ContentType, &a.Size, &a.CreatedAt); err != nil {
		return nil, err
	}
	if uploader.Valid {
		a.UploaderID = &uploader.Int64
	}
	return &a, nil
}

func (s *AttachmentStore) Create(ctx context.Context, a *model.Attachment) error {
	var uploader sql.NullInt64
	if a.UploaderID != nil {
		uploader = sql.NullInt64{Int64: *a.UploaderID, Valid: true}
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO attachments (token, repo_id, uploader_id, storage_key, content_type, size_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at`,
		a.Token, a.RepoID, uploader, a.StorageKey, a.ContentType, a.Size,
	).Scan(&a.CreatedAt)
	if err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	return nil
}

// Get wraps sql.ErrNoRows when the token is unknown.
func (s *AttachmentStore) Get(ctx context.Context, token string) (*model.Attachment, error) {
	a, err := scanAttachment(s.db.QueryRowContext(ctx, `SELECT `+attachmentColumns+` FROM attachments WHERE token = $1`, token))
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return a, nil
}

func (s *AttachmentStore) Delete(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM attachments WHERE token = $1`, token); err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	return nil
}

func (s *AttachmentStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+attachmentColumns+` FROM attachments WHERE repo_id = $1`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return collectAttachments(rows)
}

// referenced is true when a markdown body in the database holds the token.
// Wiki pages live in git and aren't searched, so the editor offers no upload
// there. A token is 128 random bits, so a substring match has no false hits.
const referenced = `(
	EXISTS (SELECT 1 FROM issues WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM comments WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM pull_requests WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM pull_reviews WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM pull_line_comments WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM discussions WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM discussion_replies WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM releases WHERE strpos(body, a.token) > 0)
	OR EXISTS (SELECT 1 FROM milestones WHERE strpos(description, a.token) > 0)
	OR EXISTS (SELECT 1 FROM saved_replies WHERE strpos(body, a.token) > 0)
)`

// ListSweepable returns up to limit attachments whose repo no longer exists,
// or that were uploaded before cutoff and that no body references.
func (s *AttachmentStore) ListSweepable(ctx context.Context, cutoff time.Time, limit int) ([]model.Attachment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.token, a.repo_id, a.uploader_id, a.storage_key, a.content_type, a.size_bytes, a.created_at
		 FROM attachments a
		 WHERE NOT EXISTS (SELECT 1 FROM repositories r WHERE r.id = a.repo_id)
		    OR (a.created_at < $1 AND NOT `+referenced+`)
		 ORDER BY a.created_at
		 LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list sweepable attachments: %w", err)
	}
	return collectAttachments(rows)
}

func collectAttachments(rows *sql.Rows) ([]model.Attachment, error) {
	defer func() { _ = rows.Close() }()
	var out []model.Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}
