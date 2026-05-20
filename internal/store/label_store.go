package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// LabelStore provides database operations for repository labels.
type LabelStore struct{ db *sql.DB }

// NewLabelStore creates a LabelStore backed by the given database.
func NewLabelStore(db *sql.DB) *LabelStore { return &LabelStore{db: db} }

func (s *LabelStore) Create(ctx context.Context, label *model.Label) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO labels (repo_id, name, color, description) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		label.RepoID, label.Name, label.Color, label.Description,
	).Scan(&label.ID, &label.CreatedAt)
	if err != nil {
		return fmt.Errorf("label create: %w", err)
	}
	return nil
}

func (s *LabelStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Label, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, name, color, description, created_at FROM labels WHERE repo_id = $1 ORDER BY name`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("label list by repo: %w", err)
	}
	defer rows.Close()
	var labels []model.Label
	for rows.Next() {
		var l model.Label
		if err := rows.Scan(&l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}

func (s *LabelStore) GetByID(ctx context.Context, id, repoID int64) (*model.Label, error) {
	l := &model.Label{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, name, color, description, created_at FROM labels WHERE id = $1 AND repo_id = $2`,
		id, repoID,
	).Scan(&l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description, &l.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("label get: %w", err)
	}
	return l, nil
}

func (s *LabelStore) Delete(ctx context.Context, id, repoID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM labels WHERE id = $1 AND repo_id = $2`, id, repoID)
	return err
}

func (s *LabelStore) AddToIssue(ctx context.Context, issueID, labelID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		issueID, labelID,
	)
	return err
}

func (s *LabelStore) RemoveFromIssue(ctx context.Context, issueID, labelID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM issue_labels WHERE issue_id = $1 AND label_id = $2`,
		issueID, labelID,
	)
	return err
}

func (s *LabelStore) ListByIssue(ctx context.Context, issueID int64) ([]model.Label, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.repo_id, l.name, l.color, l.description, l.created_at
		 FROM labels l JOIN issue_labels il ON l.id = il.label_id
		 WHERE il.issue_id = $1 ORDER BY l.name`,
		issueID,
	)
	if err != nil {
		return nil, fmt.Errorf("label list by issue: %w", err)
	}
	defer rows.Close()
	return scanLabels(rows)
}

func (s *LabelStore) AddToPull(ctx context.Context, pullID, labelID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pull_labels (pull_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		pullID, labelID,
	)
	return err
}

func (s *LabelStore) RemoveFromPull(ctx context.Context, pullID, labelID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pull_labels WHERE pull_id = $1 AND label_id = $2`,
		pullID, labelID,
	)
	return err
}

func (s *LabelStore) ListByPull(ctx context.Context, pullID int64) ([]model.Label, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.repo_id, l.name, l.color, l.description, l.created_at
		 FROM labels l JOIN pull_labels pl ON l.id = pl.label_id
		 WHERE pl.pull_id = $1 ORDER BY l.name`,
		pullID,
	)
	if err != nil {
		return nil, fmt.Errorf("label list by pull: %w", err)
	}
	defer rows.Close()
	return scanLabels(rows)
}

func (s *LabelStore) AddToDiscussion(ctx context.Context, discussionID, labelID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO discussion_labels (discussion_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		discussionID, labelID,
	)
	return err
}

func (s *LabelStore) RemoveFromDiscussion(ctx context.Context, discussionID, labelID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM discussion_labels WHERE discussion_id = $1 AND label_id = $2`,
		discussionID, labelID,
	)
	return err
}

func (s *LabelStore) ListByDiscussion(ctx context.Context, discussionID int64) ([]model.Label, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.repo_id, l.name, l.color, l.description, l.created_at
		 FROM labels l JOIN discussion_labels dl ON l.id = dl.label_id
		 WHERE dl.discussion_id = $1 ORDER BY l.name`,
		discussionID,
	)
	if err != nil {
		return nil, fmt.Errorf("label list by discussion: %w", err)
	}
	defer rows.Close()
	return scanLabels(rows)
}

// ListByIssueIDs batch-fetches labels for multiple issues. Returns a map of issueID → labels.
func (s *LabelStore) ListByIssueIDs(ctx context.Context, issueIDs []int64) (map[int64][]model.Label, error) {
	if len(issueIDs) == 0 {
		return map[int64][]model.Label{}, nil
	}
	placeholders := make([]string, len(issueIDs))
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT il.issue_id, l.id, l.repo_id, l.name, l.color, l.description, l.created_at
		 FROM labels l JOIN issue_labels il ON l.id = il.label_id
		 WHERE il.issue_id IN (%s) ORDER BY l.name`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("label list by issue ids: %w", err)
	}
	defer rows.Close()
	result := make(map[int64][]model.Label)
	for rows.Next() {
		var issueID int64
		var l model.Label
		if err := rows.Scan(&issueID, &l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
			return nil, err
		}
		result[issueID] = append(result[issueID], l)
	}
	return result, rows.Err()
}

// ListByPullIDs batch-fetches labels for multiple PRs. Returns a map of pullID → labels.
func (s *LabelStore) ListByPullIDs(ctx context.Context, pullIDs []int64) (map[int64][]model.Label, error) {
	if len(pullIDs) == 0 {
		return map[int64][]model.Label{}, nil
	}
	placeholders := make([]string, len(pullIDs))
	args := make([]any, len(pullIDs))
	for i, id := range pullIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT pl.pull_id, l.id, l.repo_id, l.name, l.color, l.description, l.created_at
		 FROM labels l JOIN pull_labels pl ON l.id = pl.label_id
		 WHERE pl.pull_id IN (%s) ORDER BY l.name`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("label list by pull ids: %w", err)
	}
	defer rows.Close()
	result := make(map[int64][]model.Label)
	for rows.Next() {
		var pullID int64
		var l model.Label
		if err := rows.Scan(&pullID, &l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
			return nil, err
		}
		result[pullID] = append(result[pullID], l)
	}
	return result, rows.Err()
}

func scanLabels(rows *sql.Rows) ([]model.Label, error) {
	var labels []model.Label
	for rows.Next() {
		var l model.Label
		if err := rows.Scan(&l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}
