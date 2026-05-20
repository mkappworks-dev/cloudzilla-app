package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// DiscussionStore provides database operations for repository discussions and replies.
type DiscussionStore struct{ db *sql.DB }

// NewDiscussionStore creates a DiscussionStore backed by the given database.
func NewDiscussionStore(db *sql.DB) *DiscussionStore { return &DiscussionStore{db: db} }

// ---- Categories ----

// Categories are a fixed, instance-wide set seeded by migration — no runtime create/delete.

func (s *DiscussionStore) ListCategories(ctx context.Context) ([]model.DiscussionCategory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, emoji, description FROM discussion_categories ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("category list: %w", err)
	}
	defer rows.Close()
	var cats []model.DiscussionCategory
	for rows.Next() {
		var c model.DiscussionCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.Emoji, &c.Description); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

func (s *DiscussionStore) GetCategory(ctx context.Context, id int64) (*model.DiscussionCategory, error) {
	var c model.DiscussionCategory
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, emoji, description FROM discussion_categories WHERE id = $1`, id,
	).Scan(&c.ID, &c.Name, &c.Emoji, &c.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

// ---- Discussions ----

// NextNumber returns the next sequential discussion number for the repo.
func (s *DiscussionStore) NextNumber(ctx context.Context, repoID int64) (int, error) {
	var max sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(number) FROM discussions WHERE repo_id = $1`, repoID,
	).Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("discussion next number: %w", err)
	}
	if max.Valid {
		return int(max.Int64) + 1, nil
	}
	return 1, nil
}

func (s *DiscussionStore) Create(ctx context.Context, d *model.Discussion) error {
	n, err := s.NextNumber(ctx, d.RepoID)
	if err != nil {
		return err
	}
	d.Number = n
	return s.db.QueryRowContext(ctx,
		`INSERT INTO discussions (repo_id, category_id, number, title, body, author_id, author_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at, updated_at`,
		d.RepoID, d.CategoryID, d.Number, d.Title, d.Body, d.AuthorID, d.AuthorName,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

func (s *DiscussionStore) List(ctx context.Context, repoID int64, categoryID int64) ([]model.Discussion, error) {
	var rows *sql.Rows
	var err error
	if categoryID == 0 {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, repo_id, category_id, number, title, body, author_id, author_name, is_locked, is_answered, answer_id, created_at, updated_at
			 FROM discussions WHERE repo_id = $1 ORDER BY created_at DESC`,
			repoID,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, repo_id, category_id, number, title, body, author_id, author_name, is_locked, is_answered, answer_id, created_at, updated_at
			 FROM discussions WHERE repo_id = $1 AND category_id = $2 ORDER BY created_at DESC`,
			repoID, categoryID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("discussion list: %w", err)
	}
	defer rows.Close()
	return scanDiscussions(rows)
}

func (s *DiscussionStore) CountByRepo(ctx context.Context, repoID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM discussions WHERE repo_id = $1`,
		repoID,
	).Scan(&n)
	return n, err
}

func (s *DiscussionStore) GetByNumber(ctx context.Context, repoID int64, number int) (*model.Discussion, error) {
	var d model.Discussion
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, category_id, number, title, body, author_id, author_name, is_locked, is_answered, answer_id, created_at, updated_at
		 FROM discussions WHERE repo_id = $1 AND number = $2`,
		repoID, number,
	).Scan(&d.ID, &d.RepoID, &d.CategoryID, &d.Number, &d.Title, &d.Body, &d.AuthorID, &d.AuthorName, &d.IsLocked, &d.IsAnswered, &d.AnswerID, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discussion get: %w", err)
	}
	return &d, nil
}

func (s *DiscussionStore) SetAnswer(ctx context.Context, discussionID int64, replyID *int64) error {
	if replyID == nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE discussions SET answer_id = NULL, is_answered = FALSE, updated_at = NOW() WHERE id = $1`,
			discussionID,
		); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx,
			`UPDATE discussion_replies SET is_answer = FALSE WHERE discussion_id = $1`,
			discussionID,
		)
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE discussions SET answer_id = $2, is_answered = TRUE, updated_at = NOW()
		 WHERE id = $1
		   AND EXISTS (SELECT 1 FROM discussion_replies WHERE id = $2 AND discussion_id = $1)`,
		discussionID, *replyID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("reply does not belong to this discussion")
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE discussion_replies SET is_answer = (id = $2) WHERE discussion_id = $1`,
		discussionID, *replyID,
	)
	return err
}

func (s *DiscussionStore) LockDiscussion(ctx context.Context, id int64, locked bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE discussions SET is_locked = $2, updated_at = NOW() WHERE id = $1`,
		id, locked,
	)
	return err
}

func (s *DiscussionStore) UpdateContent(ctx context.Context, id int64, title, body string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE discussions SET title = $2, body = $3, updated_at = NOW() WHERE id = $1`,
		id, title, body,
	)
	return err
}

func (s *DiscussionStore) SetCategory(ctx context.Context, id, categoryID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE discussions SET category_id = $2, updated_at = NOW() WHERE id = $1`,
		id, categoryID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanDiscussions(rows *sql.Rows) ([]model.Discussion, error) {
	var list []model.Discussion
	for rows.Next() {
		var d model.Discussion
		if err := rows.Scan(&d.ID, &d.RepoID, &d.CategoryID, &d.Number, &d.Title, &d.Body, &d.AuthorID, &d.AuthorName, &d.IsLocked, &d.IsAnswered, &d.AnswerID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// ---- Replies ----

func (s *DiscussionStore) CreateReply(ctx context.Context, reply *model.DiscussionReply) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO discussion_replies (discussion_id, parent_id, author_id, author_name, body)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at, updated_at`,
		reply.DiscussionID, reply.ParentID, reply.AuthorID, reply.AuthorName, reply.Body,
	).Scan(&reply.ID, &reply.CreatedAt, &reply.UpdatedAt)
}

func (s *DiscussionStore) ListReplies(ctx context.Context, discussionID int64) ([]model.DiscussionReply, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, discussion_id, parent_id, author_id, author_name, body, is_answer, created_at, updated_at
		 FROM discussion_replies WHERE discussion_id = $1 ORDER BY created_at ASC`,
		discussionID,
	)
	if err != nil {
		return nil, fmt.Errorf("reply list: %w", err)
	}
	defer rows.Close()
	var replies []model.DiscussionReply
	for rows.Next() {
		var r model.DiscussionReply
		if err := rows.Scan(&r.ID, &r.DiscussionID, &r.ParentID, &r.AuthorID, &r.AuthorName, &r.Body, &r.IsAnswer, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		replies = append(replies, r)
	}
	return replies, rows.Err()
}

func (s *DiscussionStore) DeleteReply(ctx context.Context, id, discussionID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM discussion_replies WHERE id = $1 AND discussion_id = $2`, id, discussionID)
	return err
}
