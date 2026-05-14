package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ProjectStore provides database operations for Kanban project boards, columns, and cards.
type ProjectStore struct{ db *sql.DB }

// NewProjectStore creates a ProjectStore backed by the given database.
func NewProjectStore(db *sql.DB) *ProjectStore { return &ProjectStore{db: db} }

// --- Projects ---

func (s *ProjectStore) CreateProject(ctx context.Context, p *model.Project) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO projects (repo_id, name, description)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at`,
		p.RepoID, p.Name, p.Description,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *ProjectStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, name, description, created_at, updated_at
		 FROM projects WHERE repo_id = $1 ORDER BY created_at ASC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("project list: %w", err)
	}
	defer rows.Close()
	var projects []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.RepoID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *ProjectStore) GetProject(ctx context.Context, id int64) (*model.Project, error) {
	var p model.Project
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, name, description, created_at, updated_at
		 FROM projects WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.RepoID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("project get: %w", err)
	}
	return &p, nil
}

func (s *ProjectStore) DeleteProject(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

// --- Columns ---

func (s *ProjectStore) CreateColumn(ctx context.Context, col *model.ProjectColumn) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO project_columns (project_id, name, position)
		 VALUES ($1, $2, COALESCE((SELECT MAX(position)+1 FROM project_columns WHERE project_id = $1), 0))
		 RETURNING id, position, created_at`,
		col.ProjectID, col.Name,
	).Scan(&col.ID, &col.Position, &col.CreatedAt)
}

func (s *ProjectStore) ListColumns(ctx context.Context, projectID int64) ([]model.ProjectColumn, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, position, created_at
		 FROM project_columns WHERE project_id = $1 ORDER BY position ASC, id ASC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("column list: %w", err)
	}
	defer rows.Close()
	var cols []model.ProjectColumn
	for rows.Next() {
		var c model.ProjectColumn
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Position, &c.CreatedAt); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (s *ProjectStore) UpdateColumnName(ctx context.Context, id int64, name string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE project_columns SET name = $1 WHERE id = $2`,
		name, id,
	)
	return err
}

func (s *ProjectStore) DeleteColumn(ctx context.Context, id, projectID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM project_columns WHERE id = $1 AND project_id = $2`,
		id, projectID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("column %d not found in project %d", id, projectID)
	}
	return nil
}

// --- Cards ---

func (s *ProjectStore) CreateCard(ctx context.Context, card *model.ProjectCard) error {
	issueID := sql.NullInt64{}
	if card.IssueID != nil {
		issueID = sql.NullInt64{Int64: *card.IssueID, Valid: true}
	}
	pullID := sql.NullInt64{}
	if card.PullID != nil {
		pullID = sql.NullInt64{Int64: *card.PullID, Valid: true}
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO project_cards (column_id, issue_id, pull_id, note, position)
		 VALUES ($1, $2, $3, $4,
		         COALESCE((SELECT MAX(position)+1 FROM project_cards WHERE column_id = $1), 0))
		 RETURNING id, position, created_at`,
		card.ColumnID, issueID, pullID, card.Note,
	).Scan(&card.ID, &card.Position, &card.CreatedAt)
}

func (s *ProjectStore) ListCardsByColumn(ctx context.Context, columnID int64) ([]model.ProjectCard, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT
		     c.id, c.column_id, c.issue_id, c.pull_id, c.note, c.position, c.created_at,
		     COALESCE(i.title, '')       AS issue_title,
		     COALESCE(i.number, 0)       AS issue_number,
		     COALESCE(i.state, '')       AS issue_state,
		     COALESCE(pr.title, '')      AS pull_title,
		     COALESCE(pr.number, 0)      AS pull_number,
		     COALESCE(pr.state, '')      AS pull_state
		 FROM project_cards c
		 LEFT JOIN issues       i  ON i.id  = c.issue_id
		 LEFT JOIN pull_requests pr ON pr.id = c.pull_id
		 WHERE c.column_id = $1
		 ORDER BY c.position ASC, c.id ASC`,
		columnID,
	)
	if err != nil {
		return nil, fmt.Errorf("card list: %w", err)
	}
	defer rows.Close()
	var cards []model.ProjectCard
	for rows.Next() {
		var card model.ProjectCard
		var issueID, pullID sql.NullInt64
		if err := rows.Scan(
			&card.ID, &card.ColumnID, &issueID, &pullID,
			&card.Note, &card.Position, &card.CreatedAt,
			&card.IssueTitle, &card.IssueNumber, &card.IssueState,
			&card.PullTitle, &card.PullNumber, &card.PullState,
		); err != nil {
			return nil, err
		}
		if issueID.Valid {
			card.IssueID = &issueID.Int64
		}
		if pullID.Valid {
			card.PullID = &pullID.Int64
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

// maxPositionInColumn returns the largest newPosition the caller may use for
// a move into columnID. Same-column moves keep N total cards (valid 0..N-1);
// cross-column moves grow the column to N+1 cards (valid 0..N). Excluding the
// moving card from the count handles both cases uniformly: returned value =
// (final card count in target) - 1, with -1 collapsed to 0 for empty targets.
func (s *ProjectStore) maxPositionInColumn(ctx context.Context, tx *sql.Tx, columnID, cardID int64) (int, error) {
	var others int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM project_cards WHERE column_id = $1 AND id != $2`,
		columnID, cardID,
	).Scan(&others); err != nil {
		return 0, fmt.Errorf("count target column %d: %w", columnID, err)
	}
	// Same column: final count = others + 1 (card stays), max = others.
	// Cross column: final count = others + 1 (card arrives), max = others.
	// Empty target with cross-column move: max = 0 (the single valid slot).
	return others, nil
}

// MoveCard moves a card to (newColumnID, newPosition), re-numbering siblings so
// positions stay dense (0..N-1) within each affected column. Both the source
// card and the destination column must belong to projectID; otherwise the call
// fails with sql.ErrNoRows (preventing cross-project tampering). The operation
// runs in a transaction with FOR UPDATE on the source row.
func (s *ProjectStore) MoveCard(ctx context.Context, projectID, cardID, newColumnID int64, newPosition int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin move: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var oldColumnID int64
	var oldPosition int
	if err := tx.QueryRowContext(ctx,
		`SELECT column_id, position FROM project_cards
		 WHERE id = $1
		   AND column_id IN (SELECT id FROM project_columns WHERE project_id = $2)
		 FOR UPDATE`,
		cardID, projectID,
	).Scan(&oldColumnID, &oldPosition); err != nil {
		return fmt.Errorf("locate card %d in project %d: %w", cardID, projectID, err)
	}

	upperBound, err := s.maxPositionInColumn(ctx, tx, newColumnID, cardID)
	if err != nil {
		return err
	}
	if newPosition > upperBound {
		return fmt.Errorf("position %d exceeds column size %d", newPosition, upperBound)
	}

	if oldColumnID == newColumnID {
		switch {
		case newPosition > oldPosition:
			if _, err = tx.ExecContext(ctx,
				`UPDATE project_cards SET position = position - 1
				 WHERE column_id = $1 AND position > $2 AND position <= $3 AND id != $4`,
				newColumnID, oldPosition, newPosition, cardID,
			); err != nil {
				return fmt.Errorf("reorder intra-column down: %w", err)
			}
		case newPosition < oldPosition:
			if _, err = tx.ExecContext(ctx,
				`UPDATE project_cards SET position = position + 1
				 WHERE column_id = $1 AND position >= $2 AND position < $3 AND id != $4`,
				newColumnID, newPosition, oldPosition, cardID,
			); err != nil {
				return fmt.Errorf("reorder intra-column up: %w", err)
			}
		}
	} else {
		if _, err = tx.ExecContext(ctx,
			`UPDATE project_cards SET position = position - 1
			 WHERE column_id = $1 AND position > $2`,
			oldColumnID, oldPosition,
		); err != nil {
			return fmt.Errorf("close old column gap: %w", err)
		}
		if _, err = tx.ExecContext(ctx,
			`UPDATE project_cards SET position = position + 1
			 WHERE column_id = $1 AND position >= $2`,
			newColumnID, newPosition,
		); err != nil {
			return fmt.Errorf("open new column slot: %w", err)
		}
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE project_cards SET column_id = $1, position = $2 WHERE id = $3`,
		newColumnID, newPosition, cardID,
	); err != nil {
		return fmt.Errorf("place card: %w", err)
	}
	return tx.Commit()
}

func (s *ProjectStore) DeleteCard(ctx context.Context, id, projectID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM project_cards WHERE id = $1
		 AND column_id IN (SELECT id FROM project_columns WHERE project_id = $2)`,
		id, projectID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("card %d not found in project %d", id, projectID)
	}
	return nil
}

// TouchProject updates updated_at for a project (called after card mutations).
func (s *ProjectStore) TouchProject(ctx context.Context, projectID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE projects SET updated_at = $1 WHERE id = $2`,
		time.Now().UTC(), projectID,
	)
	return err
}

// GetProjectByColumnID resolves the project that owns the given column.
func (s *ProjectStore) GetProjectByColumnID(ctx context.Context, columnID int64) (*model.Project, error) {
	var p model.Project
	err := s.db.QueryRowContext(ctx,
		`SELECT p.id, p.repo_id, p.name, p.description, p.created_at, p.updated_at
		 FROM projects p
		 JOIN project_columns c ON c.project_id = p.id
		 WHERE c.id = $1`,
		columnID,
	).Scan(&p.ID, &p.RepoID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("project by column: %w", err)
	}
	return &p, nil
}
