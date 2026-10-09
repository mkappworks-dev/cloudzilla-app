package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ErrInvalidPosition: MoveCard newPosition exceeds the destination's capacity. 400, not 500.
var ErrInvalidPosition = errors.New("invalid card position")

// ErrCardNotInProject: MoveCard source/destination doesn't belong to the claimed project.
// Distinct sentinel so the service doesn't have to overload errors.Is(sql.ErrNoRows).
var ErrCardNotInProject = errors.New("card not in project")

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

// ProjectWithCounts is a project board plus aggregate card counts, used by the
// project list view to render the item count and progress bar.
type ProjectWithCounts struct {
	model.Project
	CardCount   int // all cards (issues, PRs, notes)
	LinkedCount int // cards linked to an issue or PR
	DoneCount   int // linked cards whose issue is closed or PR merged/closed
}

// ListByRepoWithStats lists a repo's project boards with per-board card counts.
// query filters by name (case-insensitive substring; empty = no filter).
// status is "open", "closed", or "" for all.
func (s *ProjectStore) ListByRepoWithStats(ctx context.Context, repoID int64, query, status string) ([]ProjectWithCounts, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.repo_id, p.name, p.description, p.created_at, p.updated_at, p.closed_at,
		        COUNT(c.id) AS card_count,
		        COUNT(c.id) FILTER (WHERE c.issue_id IS NOT NULL OR c.pull_id IS NOT NULL) AS linked_count,
		        COUNT(c.id) FILTER (WHERE i.state = 'closed' OR pr.state IN ('merged', 'closed')) AS done_count
		 FROM projects p
		 LEFT JOIN project_columns col ON col.project_id = p.id
		 LEFT JOIN project_cards   c   ON c.column_id = col.id
		 LEFT JOIN issues          i   ON i.id = c.issue_id
		 LEFT JOIN pull_requests   pr  ON pr.id = c.pull_id
		 WHERE p.repo_id = $1
		   AND ($2 = '' OR p.name ILIKE '%' || $2 || '%')
		   AND (CASE
		            WHEN $3 = 'open'   THEN p.closed_at IS NULL
		            WHEN $3 = 'closed' THEN p.closed_at IS NOT NULL
		            ELSE TRUE
		        END)
		 GROUP BY p.id
		 ORDER BY p.created_at ASC`,
		repoID, query, status,
	)
	if err != nil {
		return nil, fmt.Errorf("project list with stats: %w", err)
	}
	defer rows.Close()
	var out []ProjectWithCounts
	for rows.Next() {
		var p ProjectWithCounts
		if err := rows.Scan(
			&p.ID, &p.RepoID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt, &p.ClosedAt,
			&p.CardCount, &p.LinkedCount, &p.DoneCount,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountByStatus returns (open, closed) project counts for a repo, honoring the
// same name filter as ListByRepoWithStats so the list tab badges stay in sync.
func (s *ProjectStore) CountByStatus(ctx context.Context, repoID int64, query string) (open, closed int, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FILTER (WHERE closed_at IS NULL),
		        COUNT(*) FILTER (WHERE closed_at IS NOT NULL)
		 FROM projects
		 WHERE repo_id = $1 AND ($2 = '' OR name ILIKE '%' || $2 || '%')`,
		repoID, query,
	).Scan(&open, &closed)
	if err != nil {
		return 0, 0, fmt.Errorf("project count by status: %w", err)
	}
	return open, closed, nil
}

// SetProjectClosed closes or reopens a board.
func (s *ProjectStore) SetProjectClosed(ctx context.Context, id int64, closed bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE projects SET closed_at = CASE WHEN $2 THEN NOW() ELSE NULL END WHERE id = $1`,
		id, closed,
	)
	return err
}

func (s *ProjectStore) GetProject(ctx context.Context, id int64) (*model.Project, error) {
	var p model.Project
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, name, description, created_at, updated_at, closed_at
		 FROM projects WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.RepoID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt, &p.ClosedAt)
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
	if card.Title == "" && card.IssueID == nil && card.PullID == nil {
		card.Title, card.Note = splitNote(card.Note)
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO project_cards (column_id, issue_id, pull_id, title, note, due_date, position)
		 VALUES ($1, $2, $3, $4, $5, $6,
		         COALESCE((SELECT MAX(position)+1 FROM project_cards WHERE column_id = $1), 0))
		 RETURNING id, position, created_at`,
		card.ColumnID, nullInt64(card.IssueID), nullInt64(card.PullID), card.Title, card.Note, nullDate(card.DueDate),
	).Scan(&card.ID, &card.Position, &card.CreatedAt)
}

const cardTitleMax = 120

// splitNote mirrors migration 111's backfill: first line becomes the title (max 120 runes), the
// rest the description. A first line over 120 runes keeps the whole text in the description.
func splitNote(note string) (title, description string) {
	note = strings.TrimSpace(note)
	first, rest, _ := strings.Cut(note, "\n")
	if r := []rune(first); len(r) > cardTitleMax {
		return string(r[:cardTitleMax]), note
	}
	return first, rest
}

func nullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func nullDate(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format("2006-01-02"), Valid: true}
}

// SetCardDetails replaces a card's editable state, assignees and labels atomically.
func (s *ProjectStore) SetCardDetails(ctx context.Context, cardID, projectID int64, d model.CardDetails) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE project_cards SET title = $3, note = $4, due_date = $5, issue_id = $6, pull_id = $7
		 WHERE id = $1 AND column_id IN (SELECT id FROM project_columns WHERE project_id = $2)`,
		cardID, projectID, d.Title, d.Description, nullDate(d.DueDate), nullInt64(d.IssueID), nullInt64(d.PullID),
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("card %d in project %d: %w", cardID, projectID, ErrCardNotInProject)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM card_assignees WHERE card_id = $1`, cardID); err != nil {
		return err
	}
	for _, id := range d.AssigneeIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO card_assignees (card_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, cardID, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM card_labels WHERE card_id = $1`, cardID); err != nil {
		return err
	}
	for _, id := range d.LabelIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO card_labels (card_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, cardID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CardAssignees returns each card's assignees ordered by username; cards without any are absent.
func (s *ProjectStore) CardAssignees(ctx context.Context, cardIDs []int64) (map[int64][]model.CardUser, error) {
	out := map[int64][]model.CardUser{}
	if len(cardIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.card_id, u.id, u.username
		 FROM card_assignees a JOIN users u ON u.id = a.user_id
		 WHERE a.card_id = ANY($1)
		 ORDER BY u.username, u.id`, cardIDs)
	if err != nil {
		return nil, fmt.Errorf("card assignees: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cardID int64
		var u model.CardUser
		if err := rows.Scan(&cardID, &u.ID, &u.Username); err != nil {
			return nil, err
		}
		out[cardID] = append(out[cardID], u)
	}
	return out, rows.Err()
}

// CardLabels returns each card's labels ordered by name; cards without any are absent.
func (s *ProjectStore) CardLabels(ctx context.Context, cardIDs []int64) (map[int64][]model.Label, error) {
	out := map[int64][]model.Label{}
	if len(cardIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT cl.card_id, l.id, l.repo_id, l.name, l.color, l.description, l.created_at
		 FROM card_labels cl JOIN labels l ON l.id = cl.label_id
		 WHERE cl.card_id = ANY($1)
		 ORDER BY l.name, l.id`, cardIDs)
	if err != nil {
		return nil, fmt.Errorf("card labels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cardID int64
		var l model.Label
		if err := rows.Scan(&cardID, &l.ID, &l.RepoID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
			return nil, err
		}
		out[cardID] = append(out[cardID], l)
	}
	return out, rows.Err()
}

// LabelsInRepo reports whether every given label belongs to repoID.
func (s *ProjectStore) LabelsInRepo(ctx context.Context, repoID int64, labelIDs []int64) (bool, error) {
	unique := map[int64]struct{}{}
	for _, id := range labelIDs {
		unique[id] = struct{}{}
	}
	if len(unique) == 0 {
		return true, nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM labels WHERE repo_id = $1 AND id = ANY($2)`, repoID, labelIDs,
	).Scan(&n); err != nil {
		return false, err
	}
	return n == len(unique), nil
}

// CardTargetsInRepo reports whether every given issue or pull request belongs to repoID.
func (s *ProjectStore) CardTargetsInRepo(ctx context.Context, repoID int64, issueID, pullID *int64) (bool, error) {
	if issueID != nil {
		var ok bool
		err := s.db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM issues WHERE id = $1 AND repo_id = $2)`, *issueID, repoID).Scan(&ok)
		if err != nil || !ok {
			return false, err
		}
	}
	if pullID != nil {
		var ok bool
		err := s.db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM pull_requests WHERE id = $1 AND repo_id = $2)`, *pullID, repoID).Scan(&ok)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

const cardSelect = `SELECT
	     c.id, c.column_id, c.issue_id, c.pull_id, c.title, c.note, c.due_date, c.position, c.created_at,
	     COALESCE(i.title, '')       AS issue_title,
	     COALESCE(i.number, 0)       AS issue_number,
	     COALESCE(i.state, '')       AS issue_state,
	     COALESCE(pr.title, '')      AS pull_title,
	     COALESCE(pr.number, 0)      AS pull_number,
	     COALESCE(pr.state, '')      AS pull_state
	 FROM project_cards c
	 LEFT JOIN issues       i  ON i.id  = c.issue_id
	 LEFT JOIN pull_requests pr ON pr.id = c.pull_id`

func scanCards(rows *sql.Rows) ([]model.ProjectCard, error) {
	var cards []model.ProjectCard
	for rows.Next() {
		var card model.ProjectCard
		var issueID, pullID sql.NullInt64
		var due sql.NullTime
		if err := rows.Scan(
			&card.ID, &card.ColumnID, &issueID, &pullID,
			&card.Title, &card.Note, &due, &card.Position, &card.CreatedAt,
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
		if due.Valid {
			card.DueDate = &due.Time
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

func (s *ProjectStore) ListCardsByColumn(ctx context.Context, columnID int64) ([]model.ProjectCard, error) {
	rows, err := s.db.QueryContext(ctx,
		cardSelect+` WHERE c.column_id = $1 ORDER BY c.position ASC, c.id ASC`, columnID)
	if err != nil {
		return nil, fmt.Errorf("card list: %w", err)
	}
	defer rows.Close()
	return scanCards(rows)
}

// GetCardInProject returns ErrCardNotInProject when the card is missing or in another project.
func (s *ProjectStore) GetCardInProject(ctx context.Context, cardID, projectID int64) (*model.ProjectCard, error) {
	rows, err := s.db.QueryContext(ctx,
		cardSelect+` WHERE c.id = $1 AND c.column_id IN (SELECT id FROM project_columns WHERE project_id = $2)`,
		cardID, projectID)
	if err != nil {
		return nil, fmt.Errorf("card get: %w", err)
	}
	defer rows.Close()
	cards, err := scanCards(rows)
	if err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("card %d in project %d: %w", cardID, projectID, ErrCardNotInProject)
	}
	return &cards[0], nil
}

// maxPositionInColumn returns the largest valid newPosition for a move into
// columnID. Excluding the moving card from the count makes same-column and
// cross-column cases match: max = others, both branches.
func (s *ProjectStore) maxPositionInColumn(ctx context.Context, tx *sql.Tx, columnID, cardID int64) (int, error) {
	var others int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM project_cards WHERE column_id = $1 AND id != $2`,
		columnID, cardID,
	).Scan(&others); err != nil {
		return 0, fmt.Errorf("count target column %d: %w", columnID, err)
	}
	return others, nil
}

// MoveCard moves a card to (newColumnID, newPosition), keeping positions dense
// (0..N-1) per column. Both source card and destination column must belong to
// projectID — enforced in SQL so the guard survives a future caller skipping
// the service authz. Returns ErrCardNotInProject or ErrInvalidPosition.
func (s *ProjectStore) MoveCard(ctx context.Context, projectID, cardID, newColumnID int64, newPosition int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin move: %w", err)
	}
	defer tx.Rollback()

	var destExists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM project_columns WHERE id = $1 AND project_id = $2)`,
		newColumnID, projectID,
	).Scan(&destExists); err != nil {
		return fmt.Errorf("check destination column %d in project %d: %w", newColumnID, projectID, err)
	}
	if !destExists {
		return fmt.Errorf("destination column %d not in project %d: %w", newColumnID, projectID, ErrCardNotInProject)
	}

	var oldColumnID int64
	var oldPosition int
	err = tx.QueryRowContext(ctx,
		`SELECT column_id, position FROM project_cards
		 WHERE id = $1
		   AND column_id IN (SELECT id FROM project_columns WHERE project_id = $2)
		 FOR UPDATE`,
		cardID, projectID,
	).Scan(&oldColumnID, &oldPosition)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("card %d not in project %d: %w", cardID, projectID, ErrCardNotInProject)
	}
	if err != nil {
		return fmt.Errorf("locate card %d in project %d: %w", cardID, projectID, err)
	}

	upperBound, err := s.maxPositionInColumn(ctx, tx, newColumnID, cardID)
	if err != nil {
		return err
	}
	if newPosition > upperBound {
		return fmt.Errorf("position %d exceeds column size %d: %w", newPosition, upperBound, ErrInvalidPosition)
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

// UpdateCardNote rewrites a note card. Linked cards have no note of their own,
// so they report ErrCardNotInProject like a card from another project.
func (s *ProjectStore) UpdateCardNote(ctx context.Context, id, projectID int64, note string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE project_cards SET note = $3
		 WHERE id = $1 AND issue_id IS NULL AND pull_id IS NULL
		 AND column_id IN (SELECT id FROM project_columns WHERE project_id = $2)`,
		id, projectID, note,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("note card %d in project %d: %w", id, projectID, ErrCardNotInProject)
	}
	return nil
}

// SearchCardTargets lists the repo's issues and pull requests whose title
// contains query or whose number equals number (0 for none), newest first.
// position() rather than ILIKE so a typed % or _ matches literally.
func (s *ProjectStore) SearchCardTargets(ctx context.Context, repoID int64, query string, number, limit int) ([]model.CardTarget, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, number, title, state FROM (
		     SELECT id, 'issue' AS kind, number, title, state, created_at FROM issues WHERE repo_id = $1
		     UNION ALL
		     SELECT id, 'pull' AS kind, number, title, state, created_at FROM pull_requests WHERE repo_id = $1
		 ) t
		 WHERE ($2 = '' OR position(lower($2) in lower(title)) > 0 OR number = $3)
		 ORDER BY created_at DESC, id DESC
		 LIMIT $4`,
		repoID, query, number, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CardTarget{}
	for rows.Next() {
		var t model.CardTarget
		if err := rows.Scan(&t.ID, &t.Kind, &t.Number, &t.Title, &t.State); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
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
		`SELECT p.id, p.repo_id, p.name, p.description, p.created_at, p.updated_at, p.closed_at
		 FROM projects p
		 JOIN project_columns c ON c.project_id = p.id
		 WHERE c.id = $1`,
		columnID,
	).Scan(&p.ID, &p.RepoID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt, &p.ClosedAt)
	if err != nil {
		return nil, fmt.Errorf("project by column: %w", err)
	}
	return &p, nil
}
