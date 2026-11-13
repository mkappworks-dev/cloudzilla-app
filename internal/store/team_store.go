package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	ErrTeamSlugTaken = errors.New("team name already taken")
	ErrNotOrgMember  = errors.New("user is not a member of the organization")
)

// TeamStore provides database operations for teams and their members.
type TeamStore struct{ db *sql.DB }

func NewTeamStore(db *sql.DB) *TeamStore { return &TeamStore{db: db} }

const teamCols = `t.id, t.org_id, t.name, t.slug, t.description, t.created_at,
	(SELECT COUNT(*) FROM team_members tm WHERE tm.team_id = t.id)`

func scanTeam(sc interface{ Scan(...any) error }, t *model.Team) error {
	return sc.Scan(&t.ID, &t.OrgID, &t.Name, &t.Slug, &t.Description, &t.CreatedAt, &t.MemberCount)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *TeamStore) Create(ctx context.Context, t *model.Team) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO teams (org_id, name, slug, description) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		t.OrgID, t.Name, t.Slug, t.Description,
	).Scan(&t.ID, &t.CreatedAt)
	if isUniqueViolation(err) {
		return ErrTeamSlugTaken
	}
	if err != nil {
		return fmt.Errorf("team create: %w", err)
	}
	return nil
}

func (s *TeamStore) GetBySlug(ctx context.Context, orgID int64, slug string) (*model.Team, error) {
	t := &model.Team{}
	err := scanTeam(s.db.QueryRowContext(ctx,
		`SELECT `+teamCols+` FROM teams t WHERE t.org_id = $1 AND t.slug = $2`, orgID, slug), t)
	if err != nil {
		return nil, fmt.Errorf("team get by slug: %w", err)
	}
	return t, nil
}

func (s *TeamStore) ListByOrg(ctx context.Context, orgID int64) ([]model.Team, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+teamCols+` FROM teams t WHERE t.org_id = $1 ORDER BY t.name ASC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("team list: %w", err)
	}
	defer rows.Close()
	var teams []model.Team
	for rows.Next() {
		var t model.Team
		if err := scanTeam(rows, &t); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// Update returns sql.ErrNoRows when the team isn't in orgID.
func (s *TeamStore) Update(ctx context.Context, orgID, id int64, name, slug, description string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE teams SET name = $3, slug = $4, description = $5 WHERE id = $2 AND org_id = $1`,
		orgID, id, name, slug, description)
	if isUniqueViolation(err) {
		return ErrTeamSlugTaken
	}
	if err != nil {
		return fmt.Errorf("team update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *TeamStore) Delete(ctx context.Context, orgID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE id = $2 AND org_id = $1`, orgID, id)
	if err != nil {
		return fmt.Errorf("team delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddMember share-locks the org row, which OrgStore.changeMember locks exclusively: a concurrent removal from the
// org either finishes first (and the membership check fails) or waits until this row exists to delete it.
func (s *TeamStore) AddMember(ctx context.Context, orgID, teamID, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("team add member begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM organizations WHERE id = $1 FOR SHARE`, orgID); err != nil {
		return fmt.Errorf("team add member lock: %w", err)
	}
	var isMember bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, orgID, userID,
	).Scan(&isMember); err != nil {
		return fmt.Errorf("team add member check: %w", err)
	}
	if !isMember {
		return ErrNotOrgMember
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id)
		 SELECT id, $3 FROM teams WHERE id = $2 AND org_id = $1
		 ON CONFLICT DO NOTHING`, orgID, teamID, userID)
	if err != nil {
		return fmt.Errorf("team add member: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("team add member commit: %w", err)
	}
	return nil
}

func (s *TeamStore) RemoveMember(ctx context.Context, orgID, teamID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM team_members WHERE team_id = $2 AND user_id = $3
		   AND team_id IN (SELECT id FROM teams WHERE id = $2 AND org_id = $1)`, orgID, teamID, userID)
	if err != nil {
		return fmt.Errorf("team remove member: %w", err)
	}
	return nil
}

func (s *TeamStore) ListMembers(ctx context.Context, teamID int64) ([]model.TeamMember, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tm.user_id, u.username FROM team_members tm JOIN users u ON u.id = tm.user_id
		 WHERE tm.team_id = $1 ORDER BY u.username ASC`, teamID)
	if err != nil {
		return nil, fmt.Errorf("team list members: %w", err)
	}
	defer rows.Close()
	var ms []model.TeamMember
	for rows.Next() {
		var m model.TeamMember
		if err := rows.Scan(&m.UserID, &m.Username); err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	return ms, rows.Err()
}

var ErrRepoNotInOrg = errors.New("repository does not belong to the team's organization")

// SetRepoGrant inserts or changes the team's role on repoID, which must be a live repo of orgID.
func (s *TeamStore) SetRepoGrant(ctx context.Context, orgID, teamID, repoID int64, role string) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO team_repos (team_id, repo_id, role)
		 SELECT t.id, r.id, $4 FROM teams t, repositories r
		  WHERE t.id = $2 AND t.org_id = $1 AND r.id = $3 AND r.org_id = $1 AND r.deleted_at IS NULL
		 ON CONFLICT (team_id, repo_id) DO UPDATE SET role = excluded.role`,
		orgID, teamID, repoID, role)
	if err != nil {
		return fmt.Errorf("team set repo grant: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRepoNotInOrg
	}
	return nil
}

func (s *TeamStore) RemoveRepoGrant(ctx context.Context, orgID, teamID, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM team_repos WHERE team_id = $2 AND repo_id = $3
		   AND team_id IN (SELECT id FROM teams WHERE id = $2 AND org_id = $1)`, orgID, teamID, repoID)
	if err != nil {
		return fmt.Errorf("team remove repo grant: %w", err)
	}
	return nil
}

// ListRepoGrants returns the team's grants on live repositories, by repo name.
func (s *TeamStore) ListRepoGrants(ctx context.Context, teamID int64) ([]model.TeamRepoGrant, error) {
	return s.queryGrants(ctx,
		`WHERE tr.team_id = $1 AND r.deleted_at IS NULL ORDER BY r.name ASC`, teamID)
}

// ListGrantsByRepo returns every team grant on repoID, by team name.
func (s *TeamStore) ListGrantsByRepo(ctx context.Context, repoID int64) ([]model.TeamRepoGrant, error) {
	return s.queryGrants(ctx, `WHERE tr.repo_id = $1 ORDER BY t.name ASC`, repoID)
}

func (s *TeamStore) queryGrants(ctx context.Context, tail string, arg int64) ([]model.TeamRepoGrant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tr.team_id, t.name, t.slug, tr.repo_id, r.name, tr.role
		   FROM team_repos tr JOIN teams t ON t.id = tr.team_id JOIN repositories r ON r.id = tr.repo_id `+tail, arg)
	if err != nil {
		return nil, fmt.Errorf("team list grants: %w", err)
	}
	defer rows.Close()
	var gs []model.TeamRepoGrant
	for rows.Next() {
		var g model.TeamRepoGrant
		if err := rows.Scan(&g.TeamID, &g.TeamName, &g.TeamSlug, &g.RepoID, &g.RepoName, &g.Role); err != nil {
			return nil, err
		}
		gs = append(gs, g)
	}
	return gs, rows.Err()
}
