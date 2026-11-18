package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RegistryStore persists registry blobs, manifests, tags and upload sessions.
type RegistryStore struct{ db *sql.DB }

func NewRegistryStore(db *sql.DB) *RegistryStore { return &RegistryStore{db: db} }

// ownerClause matches a package's path owner; $1 is the user id and $2 the organization id, one of them zero.
const ownerClause = `(($1 <> 0 AND p.owner_user_id = $1) OR ($2 <> 0 AND p.owner_org_id = $2))`

// LinkedPackage returns the id and Repository of the container package linked at the path, or sql.ErrNoRows.
func (s *RegistryStore) LinkedPackage(ctx context.Context, owner PathOwner, name string) (id, repoID int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT p.id, p.repo_id FROM packages p
		WHERE p.type = 'container' AND p.name = $3 AND p.repo_id IS NOT NULL AND `+ownerClause,
		owner.UserID, owner.OrgID, name).Scan(&id, &repoID)
	if err != nil {
		return 0, 0, fmt.Errorf("linked package: %w", err)
	}
	return id, repoID, nil
}

// PendingPackage returns creatorID's unexpired pending package at the path, or sql.ErrNoRows.
func (s *RegistryStore) PendingPackage(ctx context.Context, owner PathOwner, name string, creatorID int64, now time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT p.id FROM packages p
		WHERE p.type = 'container' AND p.name = $3 AND p.repo_id IS NULL AND p.creator_id = $4 AND p.pending_expires_at > $5
		  AND `+ownerClause, owner.UserID, owner.OrgID, name, creatorID, now).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("pending package: %w", err)
	}
	return id, nil
}

// CountPending is how many unexpired pending packages creatorID holds.
func (s *RegistryStore) CountPending(ctx context.Context, creatorID int64, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM packages
		WHERE repo_id IS NULL AND creator_id = $1 AND pending_expires_at > $2`, creatorID, now).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count pending packages: %w", err)
	}
	return n, nil
}

// DeleteExpiredPending removes creatorID's expired pending package at the path, which would otherwise hold the
// unique name, and returns the spool files of its sessions for the caller to delete.
func (s *RegistryStore) DeleteExpiredPending(ctx context.Context, owner PathOwner, name string, creatorID int64, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH gone AS (
		  DELETE FROM packages p
		  WHERE p.type = 'container' AND p.name = $3 AND p.repo_id IS NULL AND p.creator_id = $4 AND p.pending_expires_at <= $5
		    AND `+ownerClause+` RETURNING p.id)
		SELECT u.temp_path FROM package_upload_sessions u WHERE u.package_id IN (SELECT id FROM gone)`,
		owner.UserID, owner.OrgID, name, creatorID, now)
	if err != nil {
		return nil, fmt.Errorf("delete expired pending package: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan spool path: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// CreatePending inserts a pending package and returns its id; a concurrent insert of the same key returns
// ErrPendingExists.
func (s *RegistryStore) CreatePending(ctx context.Context, owner PathOwner, name string, creatorID int64, expires time.Time) (int64, error) {
	var userID, orgID any
	if owner.OrgID != 0 {
		orgID = owner.OrgID
	} else {
		userID = owner.UserID
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO packages (owner_user_id, owner_org_id, type, name, creator_id, pending_expires_at)
		VALUES ($1, $2, 'container', $3, $4, $5) ON CONFLICT DO NOTHING RETURNING id`, userID, orgID, name, creatorID, expires).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPendingExists
	}
	if err != nil {
		return 0, fmt.Errorf("create pending package: %w", err)
	}
	return id, nil
}

// ErrPendingExists is a pending package that another request created first.
var ErrPendingExists = errors.New("pending package exists")

// UploadSession is one open blob upload; Bytes is how much of the spool file is committed to it.
type UploadSession struct {
	ID        string
	PackageID int64
	UserID    int64
	Bytes     int64
	TempPath  string
	CreatedAt time.Time
	// PendingExpiresAt is set while the session's package is still pending.
	PendingExpiresAt *time.Time
}

func (s *RegistryStore) CreateSession(ctx context.Context, sess UploadSession) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO package_upload_sessions (id, package_id, user_id, temp_path, created_at)
		VALUES ($1, $2, $3, $4, $5)`, sess.ID, sess.PackageID, sess.UserID, sess.TempPath, sess.CreatedAt)
	if err != nil {
		return fmt.Errorf("create upload session: %w", err)
	}
	return nil
}

// Session returns the session with id, or sql.ErrNoRows.
func (s *RegistryStore) Session(ctx context.Context, id string) (*UploadSession, error) {
	var u UploadSession
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.package_id, u.user_id, u.bytes, u.temp_path, u.created_at,
		CASE WHEN p.repo_id IS NULL THEN p.pending_expires_at END
		FROM package_upload_sessions u JOIN packages p ON p.id = u.package_id WHERE u.id = $1`, id).
		Scan(&u.ID, &u.PackageID, &u.UserID, &u.Bytes, &u.TempPath, &u.CreatedAt, &u.PendingExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("upload session: %w", err)
	}
	return &u, nil
}

func (s *RegistryStore) SetSessionBytes(ctx context.Context, id string, bytes int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE package_upload_sessions SET bytes = $2 WHERE id = $1`, id, bytes); err != nil {
		return fmt.Errorf("set session bytes: %w", err)
	}
	return nil
}

func (s *RegistryStore) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM package_upload_sessions WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete upload session: %w", err)
	}
	return nil
}

// DeleteAllSessions drops every session; the spool they point into is gone after a restart.
func (s *RegistryStore) DeleteAllSessions(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM package_upload_sessions`); err != nil {
		return fmt.Errorf("delete upload sessions: %w", err)
	}
	return nil
}

// OpenSessions is the number of sessions userID started at or after notBefore and the bytes they hold.
func (s *RegistryStore) OpenSessions(ctx context.Context, userID int64, notBefore time.Time) (count int, bytes int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(bytes), 0) FROM package_upload_sessions
		WHERE user_id = $1 AND created_at >= $2`, userID, notBefore).Scan(&count, &bytes)
	if err != nil {
		return 0, 0, fmt.Errorf("open upload sessions: %w", err)
	}
	return count, bytes, nil
}

// BlobSize is the size of the blob packageID links at digest, or sql.ErrNoRows.
func (s *RegistryStore) BlobSize(ctx context.Context, packageID int64, digest string) (int64, error) {
	var size int64
	err := s.db.QueryRowContext(ctx, `SELECT b.size FROM package_blobs pb JOIN registry_blobs b ON b.digest = pb.digest
		WHERE pb.package_id = $1 AND pb.digest = $2`, packageID, digest).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("package blob: %w", err)
	}
	return size, nil
}

// LinkExistingBlob links a blob whose row exists, holding FOR SHARE on that row so the sweep cannot delete the
// object before the link commits. It reports false when there is no row.
func (s *RegistryStore) LinkExistingBlob(ctx context.Context, packageID int64, digest string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin link blob: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var held string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM registry_blobs WHERE digest = $1 FOR SHARE`, digest).Scan(&held)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock blob: %w", err)
	}
	if err := linkBlob(ctx, tx, packageID, digest); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// InsertAndLinkBlob records a blob whose object is already stored and links it.
func (s *RegistryStore) InsertAndLinkBlob(ctx context.Context, packageID int64, digest string, size int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin insert blob: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO registry_blobs (digest, size) VALUES ($1, $2) ON CONFLICT DO NOTHING`, digest, size); err != nil {
		return fmt.Errorf("insert blob: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM registry_blobs WHERE digest = $1 FOR SHARE`, digest); err != nil {
		return fmt.Errorf("lock blob: %w", err)
	}
	if err := linkBlob(ctx, tx, packageID, digest); err != nil {
		return err
	}
	return tx.Commit()
}

// linkBlob restarts the link's grace on a repeat, so the sweep can't take it from a push that is about to name it.
func linkBlob(ctx context.Context, tx *sql.Tx, packageID int64, digest string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO package_blobs (package_id, digest) VALUES ($1, $2) ON CONFLICT (package_id, digest) DO UPDATE SET created_at = NOW()`, packageID, digest); err != nil {
		return fmt.Errorf("link blob: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE registry_blobs SET unlinked_at = NULL WHERE digest = $1`, digest); err != nil {
		return fmt.Errorf("clear unlinked_at: %w", err)
	}
	return nil
}

// CopyBlobLink links the blob fromPackage holds into toPackage. It reports false when fromPackage has no link.
func (s *RegistryStore) CopyBlobLink(ctx context.Context, fromPackage, toPackage int64, digest string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin mount: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var held string
	err = tx.QueryRowContext(ctx, `SELECT b.digest FROM registry_blobs b JOIN package_blobs pb ON pb.digest = b.digest
		WHERE pb.package_id = $1 AND b.digest = $2 FOR SHARE OF b`, fromPackage, digest).Scan(&held)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock mounted blob: %w", err)
	}
	if err := linkBlob(ctx, tx, toPackage, digest); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// PackageRepo is the Repository a linked package belongs to.
func (s *RegistryStore) PackageRepo(ctx context.Context, packageID int64) (int64, error) {
	var repoID int64
	if err := s.db.QueryRowContext(ctx, `SELECT repo_id FROM packages WHERE id = $1 AND repo_id IS NOT NULL`, packageID).Scan(&repoID); err != nil {
		return 0, fmt.Errorf("package repo: %w", err)
	}
	return repoID, nil
}
