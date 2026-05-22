package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type PullListItem struct {
	ID           int64
	Number       int
	Title        string
	State        string
	AuthorID     int64
	RepoFullName string // "<owner_username>/<repo_name>"
	UpdatedAt    time.Time
	CreatedAt    time.Time
}

// PullStore provides database operations for pull requests.
type PullStore struct {
	db *sql.DB
}

// NewPullStore creates a PullStore backed by the given database.
func NewPullStore(database *sql.DB) *PullStore {
	return &PullStore{db: database}
}

func (s *PullStore) Create(ctx context.Context, pr *model.PullRequest) error {
	var num int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM pull_requests WHERE repo_id = $1`,
		pr.RepoID,
	).Scan(&num)
	if err != nil {
		return fmt.Errorf("pr next num: %w", err)
	}
	pr.Number = num

	var mergedAt, closedAt, draftAt sql.NullTime
	var autoMergeStrategy sql.NullString
	var headSHAOut sql.NullString
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch, is_draft, head_sha)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, created_at, updated_at, merged_at, closed_at, is_draft, draft_at, auto_merge_enabled, auto_merge_strategy, head_sha`,
		pr.RepoID, pr.Number, pr.AuthorID, pr.Title, pr.Body,
		string(pr.State), pr.HeadBranch, pr.BaseBranch, pr.IsDraft, sql.NullString{String: pr.HeadSHA, Valid: pr.HeadSHA != ""},
	).Scan(&pr.ID, &pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
		&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy, &headSHAOut)
	if headSHAOut.Valid {
		pr.HeadSHA = headSHAOut.String
	}
	if err != nil {
		return fmt.Errorf("pr create: %w", err)
	}
	if mergedAt.Valid {
		pr.MergedAt = &mergedAt.Time
	}
	if closedAt.Valid {
		pr.ClosedAt = &closedAt.Time
	}
	if draftAt.Valid {
		pr.DraftAt = &draftAt.Time
	}
	if autoMergeStrategy.Valid {
		pr.AutoMergeStrategy = autoMergeStrategy.String
	}
	return nil
}

func (s *PullStore) List(ctx context.Context, repoID int64) ([]model.PullRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.author_id, COALESCE(u.username, '') AS author_name,
		        pr.title, pr.body, pr.state, pr.head_branch, pr.base_branch,
		        pr.created_at, pr.updated_at, pr.merged_at, pr.closed_at, pr.is_draft, pr.draft_at,
		        pr.auto_merge_enabled, pr.auto_merge_strategy, pr.head_sha
		 FROM pull_requests pr
		 LEFT JOIN users u ON u.id = pr.author_id
		 WHERE pr.repo_id = $1 ORDER BY pr.number DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("pr list: %w", err)
	}
	defer rows.Close()
	return scanPullRows(rows)
}

func (s *PullStore) GetByNumber(ctx context.Context, repoID int64, number int) (*model.PullRequest, error) {
	pr := &model.PullRequest{}
	var mergedAt, closedAt, draftAt sql.NullTime
	var autoMergeStrategy, headSHA sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, number, author_id, title, body, state, head_branch, base_branch,
		        created_at, updated_at, merged_at, closed_at, is_draft, draft_at,
		        auto_merge_enabled, auto_merge_strategy, head_sha
		 FROM pull_requests WHERE repo_id = $1 AND number = $2`,
		repoID, number,
	).Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.Title, &pr.Body,
		&pr.State, &pr.HeadBranch, &pr.BaseBranch,
		&pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
		&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy, &headSHA)
	if err != nil {
		return nil, fmt.Errorf("pr get: %w", err)
	}
	if mergedAt.Valid {
		pr.MergedAt = &mergedAt.Time
	}
	if closedAt.Valid {
		pr.ClosedAt = &closedAt.Time
	}
	if draftAt.Valid {
		pr.DraftAt = &draftAt.Time
	}
	if autoMergeStrategy.Valid {
		pr.AutoMergeStrategy = autoMergeStrategy.String
	}
	if headSHA.Valid {
		pr.HeadSHA = headSHA.String
	}
	return pr, nil
}

func (s *PullStore) UpdateState(ctx context.Context, id int64, state model.PRState) error {
	now := time.Now().UTC()
	switch state {
	case model.PRStateMerged:
		_, err := s.db.ExecContext(ctx,
			`UPDATE pull_requests SET state = $1, merged_at = $2, updated_at = $3 WHERE id = $4`,
			string(state), sql.NullTime{Time: now, Valid: true}, now, id,
		)
		return err
	case model.PRStateClosed:
		_, err := s.db.ExecContext(ctx,
			`UPDATE pull_requests SET state = $1, closed_at = $2, updated_at = $3 WHERE id = $4`,
			string(state), sql.NullTime{Time: now, Valid: true}, now, id,
		)
		return err
	default:
		_, err := s.db.ExecContext(ctx,
			`UPDATE pull_requests SET state = $1, updated_at = $2 WHERE id = $3`,
			string(state), now, id,
		)
		return err
	}
}

func (s *PullStore) SetDraft(ctx context.Context, id int64, isDraft bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests
		  SET is_draft = $1,
		      draft_at = CASE WHEN $1 = TRUE THEN NOW() ELSE draft_at END,
		      updated_at = NOW()
		WHERE id = $2`,
		isDraft, id,
	)
	return err
}

func (s *PullStore) UpdateTitle(ctx context.Context, id int64, title string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests SET title = $1, updated_at = NOW() WHERE id = $2`,
		title, id,
	)
	return err
}

func (s *PullStore) UpdateBody(ctx context.Context, id int64, body string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests SET body = $1, updated_at = NOW() WHERE id = $2`,
		body, id,
	)
	return err
}

func (s *PullStore) SetAutoMerge(ctx context.Context, id int64, enabled bool, strategy string) error {
	var strat sql.NullString
	if strategy != "" {
		strat = sql.NullString{String: strategy, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests
		 SET auto_merge_enabled  = $2,
		     auto_merge_strategy = $3,
		     updated_at          = NOW()
		 WHERE id = $1`,
		id, enabled, strat,
	)
	return err
}

func (s *PullStore) ListByState(ctx context.Context, repoID int64, state model.PRState, offset, limit int) ([]model.PullRequest, error) {
	var limitParam sql.NullInt64
	if limit > 0 {
		limitParam = sql.NullInt64{Int64: int64(limit), Valid: true}
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.author_id, COALESCE(u.username, '') AS author_name,
		        pr.title, pr.body, pr.state, pr.head_branch, pr.base_branch,
		        pr.created_at, pr.updated_at, pr.merged_at, pr.closed_at, pr.is_draft, pr.draft_at,
		        pr.auto_merge_enabled, pr.auto_merge_strategy, pr.head_sha
		 FROM pull_requests pr
		 LEFT JOIN users u ON u.id = pr.author_id
		 WHERE pr.repo_id = $1 AND ($2 = '' OR pr.state = $2)
		 ORDER BY pr.number DESC LIMIT $3 OFFSET $4`,
		repoID, string(state), limitParam, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("pr list by state: %w", err)
	}
	defer rows.Close()
	return scanPullRows(rows)
}

func (s *PullStore) ListOpen(ctx context.Context, repoID int64) ([]model.PullRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.author_id, COALESCE(u.username, '') AS author_name,
		        pr.title, pr.body, pr.state, pr.head_branch, pr.base_branch,
		        pr.created_at, pr.updated_at, pr.merged_at, pr.closed_at, pr.is_draft, pr.draft_at,
		        pr.auto_merge_enabled, pr.auto_merge_strategy, pr.head_sha
		 FROM pull_requests pr
		 LEFT JOIN users u ON u.id = pr.author_id
		 WHERE pr.repo_id = $1 AND pr.state = 'open' ORDER BY pr.number DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("pr list open: %w", err)
	}
	defer rows.Close()
	return scanPullRows(rows)
}

func (s *PullStore) GetByID(ctx context.Context, id int64) (*model.PullRequest, error) {
	pr := &model.PullRequest{}
	var mergedAt, closedAt, draftAt sql.NullTime
	var autoMergeStrategy, headSHA sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, number, author_id, title, body, state, head_branch, base_branch,
		        created_at, updated_at, merged_at, closed_at, is_draft, draft_at,
		        auto_merge_enabled, auto_merge_strategy, head_sha
		 FROM pull_requests WHERE id = $1`,
		id,
	).Scan(&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.Title, &pr.Body,
		&pr.State, &pr.HeadBranch, &pr.BaseBranch,
		&pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
		&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy, &headSHA)
	if err != nil {
		return nil, fmt.Errorf("pr get by id: %w", err)
	}
	if mergedAt.Valid {
		pr.MergedAt = &mergedAt.Time
	}
	if closedAt.Valid {
		pr.ClosedAt = &closedAt.Time
	}
	if draftAt.Valid {
		pr.DraftAt = &draftAt.Time
	}
	if autoMergeStrategy.Valid {
		pr.AutoMergeStrategy = autoMergeStrategy.String
	}
	if headSHA.Valid {
		pr.HeadSHA = headSHA.String
	}
	return pr, nil
}

func scanPullRows(rows *sql.Rows) ([]model.PullRequest, error) {
	prs := []model.PullRequest{}
	for rows.Next() {
		var pr model.PullRequest
		var mergedAt, closedAt, draftAt sql.NullTime
		var autoMergeStrategy, headSHA sql.NullString
		if err := rows.Scan(
			&pr.ID, &pr.RepoID, &pr.Number, &pr.AuthorID, &pr.AuthorName, &pr.Title, &pr.Body,
			&pr.State, &pr.HeadBranch, &pr.BaseBranch,
			&pr.CreatedAt, &pr.UpdatedAt, &mergedAt, &closedAt,
			&pr.IsDraft, &draftAt, &pr.AutoMergeEnabled, &autoMergeStrategy, &headSHA,
		); err != nil {
			return nil, err
		}
		if mergedAt.Valid {
			pr.MergedAt = &mergedAt.Time
		}
		if closedAt.Valid {
			pr.ClosedAt = &closedAt.Time
		}
		if draftAt.Valid {
			pr.DraftAt = &draftAt.Time
		}
		if autoMergeStrategy.Valid {
			pr.AutoMergeStrategy = autoMergeStrategy.String
		}
		if headSHA.Valid {
			pr.HeadSHA = headSHA.String
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

func (s *PullStore) CountCreatedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1 AND created_at >= $2`,
		repoID, since,
	).Scan(&n)
	return n, err
}

func (s *PullStore) CountMergedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1 AND state = 'merged' AND merged_at >= $2`,
		repoID, since,
	).Scan(&n)
	return n, err
}

func (s *PullStore) WeeklyCreated(ctx context.Context, repoID int64, weeks int) ([]int, error) {
	weeks = clampWeeks(weeks)
	const q = `
		WITH w AS (
			SELECT generate_series(
				date_trunc('week', (NOW() AT TIME ZONE 'UTC') - ($2::int - 1) * interval '1 week'),
				date_trunc('week', (NOW() AT TIME ZONE 'UTC')),
				interval '1 week'
			) AS ws
		)
		SELECT COALESCE(COUNT(p.id), 0)::int
		FROM w
		LEFT JOIN pull_requests p ON date_trunc('week', p.created_at AT TIME ZONE 'UTC') = w.ws AND p.repo_id = $1
		GROUP BY w.ws
		ORDER BY w.ws ASC
	`
	rows, err := s.db.QueryContext(ctx, q, repoID, weeks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *PullStore) CountOpen(ctx context.Context, repoID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1 AND state = 'open'`,
		repoID,
	).Scan(&n)
	return n, err
}

func (s *PullStore) CountOpenByRepoIDs(ctx context.Context, repoIDs []int64) (map[int64]int, error) {
	if len(repoIDs) == 0 {
		return map[int64]int{}, nil
	}
	placeholders := make([]string, len(repoIDs))
	args := make([]any, len(repoIDs))
	for i, id := range repoIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := `SELECT repo_id, COUNT(*) FROM pull_requests WHERE state = 'open' AND repo_id IN (` +
		strings.Join(placeholders, ",") + `) GROUP BY repo_id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("pr count open by repo ids: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]int, len(repoIDs))
	for rows.Next() {
		var repoID int64
		var count int
		if err := rows.Scan(&repoID, &count); err != nil {
			return nil, err
		}
		result[repoID] = count
	}
	return result, rows.Err()
}

// Soft-deleted repos are excluded so the count matches the heatmap's visibility rule.
func (s *PullStore) CountOpenAssignedTo(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT p.id)
		 FROM pull_requests p
		 JOIN repositories r ON r.id = p.repo_id
		 JOIN pull_assignees a ON a.pull_id = p.id
		 WHERE p.state = 'open' AND r.deleted_at IS NULL AND a.user_id = $1`,
		userID,
	).Scan(&n)
	return n, err
}

// CountsForUser returns pull-request counts for every account-pulls tab in a
// single round-trip, keyed "<filter>:<state>". The query is composed only from
// in-code constants — never caller input — so the concatenation is injection-safe.
func (s *PullStore) CountsForUser(ctx context.Context, userID int64) (map[string]int, error) {
	const vis = `(NOT r.private OR r.owner_id = $1
	              OR EXISTS (SELECT 1 FROM permissions perm WHERE perm.repo_id = r.id AND perm.user_id = $1))`
	const (
		created         = `p.author_id = $1`
		assigned        = `EXISTS (SELECT 1 FROM pull_assignees pa WHERE pa.pull_id = p.id AND pa.user_id = $1)`
		reviewRequested = `EXISTS (SELECT 1 FROM pull_reviews prv WHERE prv.pull_id = p.id AND prv.author_id = $1 AND prv.state = 'pending')`
		mentioned       = `EXISTS (SELECT 1 FROM mentions m JOIN comments c ON c.id = m.comment_id WHERE c.pull_id = p.id AND m.user_id = $1)`
	)
	sub := func(state, cond string) string {
		return `(SELECT COUNT(*) FROM pull_requests p JOIN repositories r ON r.id = p.repo_id
		         WHERE r.deleted_at IS NULL AND p.state = '` + state + `' AND ` + cond + ` AND ` + vis + `)`
	}
	q := `SELECT ` +
		sub("open", created) + `, ` + sub("closed", created) + `, ` +
		sub("open", assigned) + `, ` + sub("closed", assigned) + `, ` +
		sub("open", reviewRequested) + `, ` + sub("closed", reviewRequested) + `, ` +
		sub("open", mentioned) + `, ` + sub("closed", mentioned)
	var co, cc, ao, ac, ro, rc, mo, mc int
	if err := s.db.QueryRowContext(ctx, q, userID).Scan(&co, &cc, &ao, &ac, &ro, &rc, &mo, &mc); err != nil {
		return nil, fmt.Errorf("pull counts for user: %w", err)
	}
	return map[string]int{
		"created:open": co, "created:closed": cc,
		"assigned:open": ao, "assigned:closed": ac,
		"review_requested:open": ro, "review_requested:closed": rc,
		"mentioned:open": mo, "mentioned:closed": mc,
	}, nil
}

func (s *PullStore) ListLinkedToIssue(ctx context.Context, repoID int64, issueNumber int) ([]model.PullRequest, error) {
	// Explicit links from the pull_issue_links table — the same set the issue
	// sidebar's link/unlink dropdown writes to.
	rows, err := s.db.QueryContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.author_id, COALESCE(u.username, '') AS author_name,
		        pr.title, pr.body, pr.state, pr.head_branch, pr.base_branch,
		        pr.created_at, pr.updated_at, pr.merged_at, pr.closed_at, pr.is_draft, pr.draft_at,
		        pr.auto_merge_enabled, pr.auto_merge_strategy, pr.head_sha
		 FROM pull_requests pr
		 LEFT JOIN users u ON u.id = pr.author_id
		 JOIN pull_issue_links pil ON pil.pull_id = pr.id
		 JOIN issues i ON i.id = pil.issue_id
		 WHERE pr.repo_id = $1 AND i.repo_id = $1 AND i.number = $2
		 ORDER BY pr.number DESC`,
		repoID, issueNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("pr linked to issue: %w", err)
	}
	defer rows.Close()
	return scanPullRows(rows)
}

// AssignedAtForUser returns a map of pull_id → assignment created_at for all
// open PRs assigned to userID. Used alongside ListForUser("assigned") to
// populate WaitingSince without changing the shared PullListItem type.
func (s *PullStore) AssignedAtForUser(ctx context.Context, userID int64) (map[int64]time.Time, error) {
	const q = `
		SELECT pa.pull_id, pa.created_at
		FROM pull_assignees pa
		JOIN pull_requests p  ON p.id = pa.pull_id
		JOIN repositories r   ON r.id = p.repo_id
		WHERE pa.user_id = $1 AND p.state = 'open' AND r.deleted_at IS NULL
	`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[int64]time.Time)
	for rows.Next() {
		var id int64
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		m[id] = t
	}
	return m, rows.Err()
}

// mode is "created" or "assigned"; state is "open" or "closed". For "review_requested" and "mentioned", use ListByIDs.
func (s *PullStore) ListForUser(ctx context.Context, userID int64, mode, state string) ([]PullListItem, error) {
	join, cond := "", ""
	switch mode {
	case "assigned":
		join = `JOIN pull_assignees pa ON pa.pull_id = p.id`
		cond = `pa.user_id = $1`
	default: // "created"
		cond = `p.author_id = $1`
	}
	q := `SELECT DISTINCT p.id, p.number, p.title, p.state, p.author_id,
	             u.username || '/' || r.name AS repo_full_name, p.updated_at, p.created_at
	      FROM pull_requests p
	      JOIN repositories r ON r.id = p.repo_id
	      JOIN users u        ON u.id = r.owner_id
	      ` + join + `
	      WHERE r.deleted_at IS NULL AND p.state = $2 AND ` + cond + `
	        AND (NOT r.private OR r.owner_id = $1
	             OR EXISTS (SELECT 1 FROM permissions perm WHERE perm.repo_id = r.id AND perm.user_id = $1))
	      ORDER BY p.updated_at DESC LIMIT 100`
	return s.scanPullListItems(ctx, q, userID, state)
}

// Restricted to repos visible to userID — the ID sets can include PRs in private repos the user cannot read.
func (s *PullStore) ListByIDs(ctx context.Context, userID int64, ids []int64, state string) ([]PullListItem, error) {
	if len(ids) == 0 {
		return []PullListItem{}, nil
	}
	placeholders := make([]string, len(ids))
	args := []any{userID, state}
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, id)
	}
	q := `SELECT DISTINCT p.id, p.number, p.title, p.state, p.author_id,
	             u.username || '/' || r.name AS repo_full_name, p.updated_at, p.created_at
	      FROM pull_requests p
	      JOIN repositories r ON r.id = p.repo_id
	      JOIN users u        ON u.id = r.owner_id
	      WHERE r.deleted_at IS NULL AND p.state = $2
	        AND p.id IN (` + strings.Join(placeholders, ",") + `)
	        AND (NOT r.private OR r.owner_id = $1
	             OR EXISTS (SELECT 1 FROM permissions perm WHERE perm.repo_id = r.id AND perm.user_id = $1))
	      ORDER BY p.updated_at DESC LIMIT 100`
	return s.scanPullListItems(ctx, q, args...)
}

func (s *PullStore) scanPullListItems(ctx context.Context, q string, args ...any) ([]PullListItem, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("scan pull list items: %w", err)
	}
	defer rows.Close()
	out := []PullListItem{}
	for rows.Next() {
		var it PullListItem
		if err := rows.Scan(&it.ID, &it.Number, &it.Title, &it.State, &it.AuthorID, &it.RepoFullName, &it.UpdatedAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *PullStore) UpdateHeadSHA(ctx context.Context, pullID int64, sha string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests SET head_sha = $2 WHERE id = $1`,
		pullID, sha,
	)
	return err
}

func (s *PullStore) UpdateHeadSHAByBranch(ctx context.Context, repoID int64, headBranch, sha string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pull_requests SET head_sha = $3 WHERE repo_id = $1 AND head_branch = $2 AND state = 'open'`,
		repoID, headBranch, sha,
	)
	return err
}

// GetManyByIDs fetches full PullRequest rows by a set of IDs. Used for batching CI-check lookups.
func (s *PullStore) GetManyByIDs(ctx context.Context, ids []int64) ([]model.PullRequest, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.author_id, COALESCE(u.username, '') AS author_name,
		        pr.title, pr.body, pr.state, pr.head_branch, pr.base_branch,
		        pr.created_at, pr.updated_at, pr.merged_at, pr.closed_at, pr.is_draft, pr.draft_at,
		        pr.auto_merge_enabled, pr.auto_merge_strategy, pr.head_sha
		 FROM pull_requests pr
		 LEFT JOIN users u ON u.id = pr.author_id
		 WHERE pr.id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("pr get many by ids: %w", err)
	}
	defer rows.Close()
	return scanPullRows(rows)
}
