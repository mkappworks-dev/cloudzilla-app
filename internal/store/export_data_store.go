package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// The export row types are what ends up in the zip, so a new column on a source table never reaches an export
// unless it is added here.

type ExportProfile struct {
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Bio       string    `json:"bio"`
	Company   string    `json:"company"`
	Location  string    `json:"location"`
	CreatedAt time.Time `json:"created_at"`
}

type ExportEmail struct {
	Email      string     `json:"email"`
	VerifiedAt *time.Time `json:"verified_at"`
}

type ExportSSHKey struct {
	Title       string    `json:"title"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

type ExportGPGKey struct {
	KeyID       string     `json:"key_id"`
	Fingerprint string     `json:"fingerprint"`
	PublicKey   string     `json:"public_key"`
	ExpiresAt   *time.Time `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	RemovedAt   *time.Time `json:"removed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type ExportSSHSigningKey struct {
	ExportSSHKey
	RemovedAt *time.Time `json:"removed_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

type ExportIssue struct {
	Repository string     `json:"repository"`
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ClosedAt   *time.Time `json:"closed_at"`
}

type ExportComment struct {
	Repository string    `json:"repository"`
	On         string    `json:"on"`
	Number     int       `json:"number"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ExportPull struct {
	Repository string     `json:"repository"`
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	State      string     `json:"state"`
	HeadBranch string     `json:"head_branch"`
	BaseBranch string     `json:"base_branch"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	MergedAt   *time.Time `json:"merged_at"`
	ClosedAt   *time.Time `json:"closed_at"`
}

type ExportReview struct {
	Repository  string     `json:"repository"`
	Pull        int        `json:"pull_request"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	SubmittedAt *time.Time `json:"submitted_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type ExportLineComment struct {
	Repository string    `json:"repository"`
	Pull       int       `json:"pull_request"`
	Path       string    `json:"path"`
	Side       string    `json:"side"`
	Line       int       `json:"line"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type ExportGistFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type ExportGist struct {
	ID          string           `json:"id"`
	Description string           `json:"description"`
	Public      bool             `json:"public"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Files       []ExportGistFile `json:"files"`
}

type ExportStar struct {
	Repository string    `json:"repository"`
	StarredAt  time.Time `json:"starred_at"`
}

type ExportWatch struct {
	Repository string    `json:"repository"`
	Level      string    `json:"level"`
	WatchedAt  time.Time `json:"watched_at"`
}

// eachRow streams a query through fn one row at a time, so an export's memory does not grow with the data.
func eachRow[T any](ctx context.Context, db *sql.DB, query string, args []any, scan func(*sql.Rows) (T, error), fn func(T) error) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("export query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return fmt.Errorf("export scan: %w", err)
		}
		if err := fn(v); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *ExportStore) Profile(ctx context.Context, userID int64) (ExportProfile, []ExportEmail, error) {
	var p ExportProfile
	if err := s.db.QueryRowContext(ctx,
		`SELECT username, name, email, bio, company, location, created_at FROM users WHERE id = $1`, userID,
	).Scan(&p.Username, &p.Name, &p.Email, &p.Bio, &p.Company, &p.Location, &p.CreatedAt); err != nil {
		return p, nil, fmt.Errorf("export profile: %w", err)
	}
	emails := []ExportEmail{}
	err := eachRow(ctx, s.db, `SELECT email, verified_at FROM user_emails WHERE user_id = $1 ORDER BY id`, []any{userID},
		func(r *sql.Rows) (ExportEmail, error) {
			var e ExportEmail
			return e, r.Scan(&e.Email, &e.VerifiedAt)
		}, func(e ExportEmail) error { emails = append(emails, e); return nil })
	return p, emails, err
}

func (s *ExportStore) EachSSHKey(ctx context.Context, userID int64, fn func(ExportSSHKey) error) error {
	return eachRow(ctx, s.db, `SELECT title, public_key, fingerprint, created_at FROM ssh_keys WHERE user_id = $1 ORDER BY id`, []any{userID},
		func(r *sql.Rows) (ExportSSHKey, error) {
			var k ExportSSHKey
			return k, r.Scan(&k.Title, &k.PublicKey, &k.Fingerprint, &k.CreatedAt)
		}, fn)
}

func (s *ExportStore) EachGPGKey(ctx context.Context, userID int64, fn func(ExportGPGKey) error) error {
	return eachRow(ctx, s.db, `SELECT key_id, fingerprint, public_key, expires_at, revoked_at, removed_at, created_at FROM gpg_keys WHERE user_id = $1 ORDER BY id`, []any{userID},
		func(r *sql.Rows) (ExportGPGKey, error) {
			var k ExportGPGKey
			return k, r.Scan(&k.KeyID, &k.Fingerprint, &k.PublicKey, &k.ExpiresAt, &k.RevokedAt, &k.RemovedAt, &k.CreatedAt)
		}, fn)
}

func (s *ExportStore) EachSSHSigningKey(ctx context.Context, userID int64, fn func(ExportSSHSigningKey) error) error {
	return eachRow(ctx, s.db, `SELECT title, public_key, fingerprint, removed_at, revoked_at, created_at FROM ssh_signing_keys WHERE user_id = $1 ORDER BY id`, []any{userID},
		func(r *sql.Rows) (ExportSSHSigningKey, error) {
			var k ExportSSHSigningKey
			return k, r.Scan(&k.Title, &k.PublicKey, &k.Fingerprint, &k.RemovedAt, &k.RevokedAt, &k.CreatedAt)
		}, fn)
}

// The repositories join leaves out those in the trash, whose names the user no longer has a way to see.
func (s *ExportStore) EachIssue(ctx context.Context, userID int64, fn func(ExportIssue) error) error {
	return eachRow(ctx, s.db,
		`SELECT r.owner_name || '/' || r.name, i.number, i.title, i.body, i.state, i.created_at, i.updated_at, i.closed_at
		   FROM issues i JOIN repositories r ON r.id = i.repo_id
		  WHERE i.author_id = $1 AND r.deleted_at IS NULL ORDER BY i.id`, []any{userID},
		func(r *sql.Rows) (ExportIssue, error) {
			var v ExportIssue
			return v, r.Scan(&v.Repository, &v.Number, &v.Title, &v.Body, &v.State, &v.CreatedAt, &v.UpdatedAt, &v.ClosedAt)
		}, fn)
}

func (s *ExportStore) EachComment(ctx context.Context, userID int64, fn func(ExportComment) error) error {
	return eachRow(ctx, s.db,
		`SELECT r.owner_name || '/' || r.name,
		        CASE WHEN c.issue_id IS NOT NULL THEN 'issue' ELSE 'pull_request' END,
		        COALESCE(i.number, p.number, 0), c.body, c.created_at, c.updated_at
		   FROM comments c
		   JOIN repositories r ON r.id = c.repo_id
		   LEFT JOIN issues i ON i.id = c.issue_id
		   LEFT JOIN pull_requests p ON p.id = c.pull_id
		  WHERE c.author_id = $1 AND r.deleted_at IS NULL ORDER BY c.id`, []any{userID},
		func(r *sql.Rows) (ExportComment, error) {
			var v ExportComment
			return v, r.Scan(&v.Repository, &v.On, &v.Number, &v.Body, &v.CreatedAt, &v.UpdatedAt)
		}, fn)
}

func (s *ExportStore) EachPull(ctx context.Context, userID int64, fn func(ExportPull) error) error {
	return eachRow(ctx, s.db,
		`SELECT r.owner_name || '/' || r.name, p.number, p.title, p.body, p.state, p.head_branch, p.base_branch,
		        p.created_at, p.updated_at, p.merged_at, p.closed_at
		   FROM pull_requests p JOIN repositories r ON r.id = p.repo_id
		  WHERE p.author_id = $1 AND r.deleted_at IS NULL ORDER BY p.id`, []any{userID},
		func(r *sql.Rows) (ExportPull, error) {
			var v ExportPull
			return v, r.Scan(&v.Repository, &v.Number, &v.Title, &v.Body, &v.State, &v.HeadBranch, &v.BaseBranch,
				&v.CreatedAt, &v.UpdatedAt, &v.MergedAt, &v.ClosedAt)
		}, fn)
}

func (s *ExportStore) EachReview(ctx context.Context, userID int64, fn func(ExportReview) error) error {
	return eachRow(ctx, s.db,
		`SELECT r.owner_name || '/' || r.name, p.number, v.state, v.body, v.submitted_at, v.created_at
		   FROM pull_reviews v
		   JOIN pull_requests p ON p.id = v.pull_id
		   JOIN repositories r ON r.id = v.repo_id
		  WHERE v.author_id = $1 AND r.deleted_at IS NULL ORDER BY v.id`, []any{userID},
		func(r *sql.Rows) (ExportReview, error) {
			var v ExportReview
			return v, r.Scan(&v.Repository, &v.Pull, &v.State, &v.Body, &v.SubmittedAt, &v.CreatedAt)
		}, fn)
}

func (s *ExportStore) EachLineComment(ctx context.Context, userID int64, fn func(ExportLineComment) error) error {
	return eachRow(ctx, s.db,
		`SELECT r.owner_name || '/' || r.name, p.number, c.path, c.diff_side, c.line, c.body, c.created_at
		   FROM pull_line_comments c
		   JOIN pull_requests p ON p.id = c.pull_id
		   JOIN repositories r ON r.id = c.repo_id
		  WHERE c.author_id = $1 AND r.deleted_at IS NULL ORDER BY c.id`, []any{userID},
		func(r *sql.Rows) (ExportLineComment, error) {
			var v ExportLineComment
			return v, r.Scan(&v.Repository, &v.Pull, &v.Path, &v.Side, &v.Line, &v.Body, &v.CreatedAt)
		}, fn)
}

// Each gist's files are read after its row, so only one gist's files are in memory at a time.
func (s *ExportStore) EachGist(ctx context.Context, userID int64, fn func(ExportGist) error) error {
	gists, err := s.db.QueryContext(ctx,
		`SELECT id, description, public, created_at, updated_at FROM gists WHERE owner_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return fmt.Errorf("export gists: %w", err)
	}
	defer gists.Close()
	for gists.Next() {
		g := ExportGist{Files: []ExportGistFile{}}
		if err := gists.Scan(&g.ID, &g.Description, &g.Public, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return fmt.Errorf("export gist scan: %w", err)
		}
		err := eachRow(ctx, s.db, `SELECT filename, content FROM gist_files WHERE gist_id = $1 ORDER BY filename`, []any{g.ID},
			func(r *sql.Rows) (ExportGistFile, error) {
				var f ExportGistFile
				return f, r.Scan(&f.Filename, &f.Content)
			}, func(f ExportGistFile) error { g.Files = append(g.Files, f); return nil })
		if err != nil {
			return err
		}
		if err := fn(g); err != nil {
			return err
		}
	}
	return gists.Err()
}

// Stars and watches are listed only for repositories userID can still read, so a private name does not outlive access.
func (s *ExportStore) EachStar(ctx context.Context, userID int64, fn func(ExportStar) error) error {
	return eachRow(ctx, s.db,
		`SELECT r.owner_name || '/' || r.name, st.created_at
		   FROM stars st JOIN repositories r ON r.id = st.repo_id
		  WHERE st.user_id = $1 AND r.deleted_at IS NULL AND `+readableBy("r", "$1")+` ORDER BY st.created_at, r.id`, []any{userID},
		func(r *sql.Rows) (ExportStar, error) {
			var v ExportStar
			return v, r.Scan(&v.Repository, &v.StarredAt)
		}, fn)
}

func (s *ExportStore) EachWatch(ctx context.Context, userID int64, fn func(ExportWatch) error) error {
	return eachRow(ctx, s.db,
		`SELECT r.owner_name || '/' || r.name, w.level, w.created_at
		   FROM watches w JOIN repositories r ON r.id = w.repo_id
		  WHERE w.user_id = $1 AND r.deleted_at IS NULL AND `+readableBy("r", "$1")+` ORDER BY w.id`, []any{userID},
		func(r *sql.Rows) (ExportWatch, error) {
			var v ExportWatch
			return v, r.Scan(&v.Repository, &v.Level, &v.WatchedAt)
		}, fn)
}
