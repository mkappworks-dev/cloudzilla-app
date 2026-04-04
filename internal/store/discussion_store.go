package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type DiscussionStore struct{ db *sql.DB }

func NewDiscussionStore(db *sql.DB) *DiscussionStore { return &DiscussionStore{db: db} }

// ---- Categories ----

func (s *DiscussionStore) CreateCategory(ctx context.Context, c *model.DiscussionCategory) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO discussion_categories (repo_id, name, emoji) VALUES ($1, $2, $3) RETURNING id`,
		c.RepoID, c.Name, c.Emoji,
	).Scan(&c.ID)
}

func (s *DiscussionStore) ListCategories(ctx context.Context, repoID int64) ([]model.DiscussionCategory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, name, emoji FROM discussion_categories WHERE repo_id = $1 ORDER BY name`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("category list: %w", err)
	}
	defer rows.Close()
	var cats []model.DiscussionCategory
	for rows.Next() {
		var c model.DiscussionCategory
		if err := rows.Scan(&c.ID, &c.RepoID, &c.Name, &c.Emoji); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

func (s *DiscussionStore) GetCategory(ctx context.Context, id int64) (*model.DiscussionCategory, error) {
	var c model.DiscussionCategory
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, name, emoji FROM discussion_categories WHERE id = $1`, id,
	).Scan(&c.ID, &c.RepoID, &c.Name, &c.Emoji)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

func (s *DiscussionStore) DeleteCategory(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM discussion_categories WHERE id = $1`, id)
	return err
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
		_, err := s.db.ExecContext(ctx,
			`UPDATE discussions SET answer_id = NULL, is_answered = FALSE, updated_at = NOW() WHERE id = $1`,
			discussionID,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE discussions SET answer_id = $2, is_answered = TRUE, updated_at = NOW() WHERE id = $1`,
		discussionID, *replyID,
	)
	if err != nil {
		return err
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

func (s *DiscussionStore) DeleteReply(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM discussion_replies WHERE id = $1`, id)
	return err
}
