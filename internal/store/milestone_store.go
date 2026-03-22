package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type MilestoneStore struct{ db *sql.DB }

func NewMilestoneStore(db *sql.DB) *MilestoneStore { return &MilestoneStore{db: db} }

const milestoneCountsSQL = `
SELECT m.id, m.repo_id, m.number, m.title, m.description, m.state,
       m.due_date, m.closed_at, m.created_at, m.updated_at,
       (SELECT COUNT(*) FROM issues i WHERE i.milestone_id = m.id AND i.state = 'open')   AS open_count,
       (SELECT COUNT(*) FROM issues i WHERE i.milestone_id = m.id AND i.state = 'closed') AS closed_count
FROM milestones m`

func (s *MilestoneStore) Create(ctx context.Context, m *model.Milestone) error {
	const q = `
INSERT INTO milestones (repo_id, number, title, description, state, due_date)
VALUES ($1,
        (SELECT COALESCE(MAX(number), 0) + 1 FROM milestones WHERE repo_id = $1),
        $2, $3, 'open', $4)
RETURNING id, number, created_at, updated_at`

	var dueDate sql.NullTime
	if m.DueDate != nil {
		dueDate = sql.NullTime{Time: *m.DueDate, Valid: true}
	}
	return s.db.QueryRowContext(ctx, q,
		m.RepoID, m.Title, m.Description, dueDate,
	).Scan(&m.ID, &m.Number, &m.CreatedAt, &m.UpdatedAt)
}

func (s *MilestoneStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Milestone, error) {
	rows, err := s.db.QueryContext(ctx,
		milestoneCountsSQL+` WHERE m.repo_id = $1 ORDER BY m.created_at DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("milestone list: %w", err)
	}
	defer rows.Close()
	return scanMilestones(rows)
}

func (s *MilestoneStore) GetByNumber(ctx context.Context, repoID int64, number int) (*model.Milestone, error) {
	row := s.db.QueryRowContext(ctx,
		milestoneCountsSQL+` WHERE m.repo_id = $1 AND m.number = $2`,
		repoID, number,
	)
	m, err := scanMilestone(row)
	if err != nil {
		return nil, fmt.Errorf("milestone get: %w", err)
	}
	return m, nil
}

func (s *MilestoneStore) GetByID(ctx context.Context, id int64) (*model.Milestone, error) {
	row := s.db.QueryRowContext(ctx,
		milestoneCountsSQL+` WHERE m.id = $1`,
		id,
	)
	m, err := scanMilestone(row)
	if err != nil {
		return nil, fmt.Errorf("milestone get by id: %w", err)
	}
	return m, nil
}

func (s *MilestoneStore) Update(ctx context.Context, m *model.Milestone) error {
	const q = `
UPDATE milestones SET title=$1, description=$2, state=$3, due_date=$4, closed_at=$5, updated_at=NOW()
WHERE id=$6 AND repo_id=$7
RETURNING updated_at`

	var dueDate sql.NullTime
	if m.DueDate != nil {
		dueDate = sql.NullTime{Time: *m.DueDate, Valid: true}
	}
	var closedAt sql.NullTime
	if m.ClosedAt != nil {
		closedAt = sql.NullTime{Time: *m.ClosedAt, Valid: true}
	}
	return s.db.QueryRowContext(ctx, q,
		m.Title, m.Description, m.State, dueDate, closedAt, m.ID, m.RepoID,
	).Scan(&m.UpdatedAt)
}

func (s *MilestoneStore) Delete(ctx context.Context, repoID int64, number int) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM milestones WHERE repo_id = $1 AND number = $2`,
		repoID, number,
	)
	return err
}

// scanMilestone scans a single milestone row including computed open/closed counts.
func scanMilestone(row *sql.Row) (*model.Milestone, error) {
	var m model.Milestone
	var dueDate, closedAt sql.NullTime
	if err := row.Scan(
		&m.ID, &m.RepoID, &m.Number, &m.Title, &m.Description, &m.State,
		&dueDate, &closedAt, &m.CreatedAt, &m.UpdatedAt,
		&m.OpenCount, &m.ClosedCount,
	); err != nil {
		return nil, err
	}
	if dueDate.Valid {
		t := dueDate.Time
		m.DueDate = &t
	}
	if closedAt.Valid {
		t := closedAt.Time
		m.ClosedAt = &t
	}
	return &m, nil
}

func scanMilestones(rows *sql.Rows) ([]model.Milestone, error) {
	var milestones []model.Milestone
	for rows.Next() {
		var m model.Milestone
		var dueDate, closedAt sql.NullTime
		if err := rows.Scan(
			&m.ID, &m.RepoID, &m.Number, &m.Title, &m.Description, &m.State,
			&dueDate, &closedAt, &m.CreatedAt, &m.UpdatedAt,
			&m.OpenCount, &m.ClosedCount,
		); err != nil {
			return nil, err
		}
		if dueDate.Valid {
			t := dueDate.Time
			m.DueDate = &t
		}
		if closedAt.Valid {
			t := closedAt.Time
			m.ClosedAt = &t
		}
		milestones = append(milestones, m)
	}
	return milestones, rows.Err()
}

// SetMilestone on issues
func (s *MilestoneStore) SetIssue(ctx context.Context, issueID int64, milestoneID *int64) error {
	var mid sql.NullInt64
	if milestoneID != nil {
		mid = sql.NullInt64{Int64: *milestoneID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE issues SET milestone_id=$1, updated_at=NOW() WHERE id=$2`,
		mid, issueID,
	)
	return err
}

// SetMilestone on pull_requests
func (s *MilestoneStore) SetPull(ctx context.Context, pullID int64, milestoneID *int64) error {
	var mid sql.NullInt64
	if milestoneID != nil {
		mid = sql.NullInt64{Int64: *milestoneID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests SET milestone_id=$1, updated_at=NOW() WHERE id=$2`,
		mid, pullID,
	)
	return err
}

// GetIssueID returns the milestone_id for an issue (nil if unset).
func (s *MilestoneStore) GetIssueID(ctx context.Context, issueID int64) (*int64, error) {
	var mid sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT milestone_id FROM issues WHERE id=$1`, issueID,
	).Scan(&mid)
	if err != nil {
		return nil, err
	}
	if !mid.Valid {
		return nil, nil
	}
	v := mid.Int64
	return &v, nil
}

// GetPullID returns the milestone_id for a pull request (nil if unset).
func (s *MilestoneStore) GetPullID(ctx context.Context, pullID int64) (*int64, error) {
	var mid sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT milestone_id FROM pull_requests WHERE id=$1`, pullID,
	).Scan(&mid)
	if err != nil {
		return nil, err
	}
	if !mid.Valid {
		return nil, nil
	}
	v := mid.Int64
	return &v, nil
}

// ListIssuesByMilestone returns issues belonging to a milestone.
func (s *MilestoneStore) ListIssuesByMilestone(ctx context.Context, milestoneID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM issues WHERE milestone_id=$1`, milestoneID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

