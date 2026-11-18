package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PackageStore persists packages and their generic versions and files.
type PackageStore struct{ db *sql.DB }

func NewPackageStore(db *sql.DB) *PackageStore { return &PackageStore{db: db} }

// OwnerUsage is the bytes a path owner pays for: each distinct blob linked from its packages once (pending
// packages included), its generic files, and the bytes of its open upload sessions.
func (s *PackageStore) OwnerUsage(ctx context.Context, userID, orgID int64) (int64, error) {
	col, id := "owner_user_id", userID
	if orgID != 0 {
		col, id = "owner_org_id", orgID
	}
	var used int64
	err := s.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COALESCE(SUM(b.size), 0) FROM registry_blobs b WHERE b.digest IN
		     (SELECT pb.digest FROM package_blobs pb JOIN packages p ON p.id = pb.package_id WHERE p.`+col+` = $1))
		+ (SELECT COALESCE(SUM(f.size), 0) FROM package_files f
		     JOIN package_versions v ON v.id = f.version_id JOIN packages p ON p.id = v.package_id WHERE p.`+col+` = $1)
		+ (SELECT COALESCE(SUM(u.bytes), 0) FROM package_upload_sessions u
		     JOIN packages p ON p.id = u.package_id WHERE p.`+col+` = $1)`, id).Scan(&used)
	if err != nil {
		return 0, fmt.Errorf("package usage: %w", err)
	}
	return used, nil
}

// PathOwner is the user or organization a package path starts with; exactly one of the ids is set.
type PathOwner struct {
	UserID int64
	OrgID  int64
	Name   string
}

// ResolveOwner finds the user or organization a path's first segment names, users first, ignoring case.
func (s *PackageStore) ResolveOwner(ctx context.Context, segment string) (PathOwner, error) {
	var o PathOwner
	err := s.db.QueryRowContext(ctx, `SELECT id, username FROM users WHERE lower(username) = lower($1) AND `+notGhost, segment).Scan(&o.UserID, &o.Name)
	if err == nil {
		return o, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PathOwner{}, fmt.Errorf("resolve path owner: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `SELECT id, name FROM organizations WHERE lower(name) = lower($1)`, segment).Scan(&o.OrgID, &o.Name)
	if err != nil {
		return PathOwner{}, fmt.Errorf("resolve path owner: %w", err)
	}
	return o, nil
}

// LinkedRepoID returns the repository the container package at the path is linked to, or sql.ErrNoRows.
func (s *PackageStore) LinkedRepoID(ctx context.Context, userID, orgID int64, name string) (int64, error) {
	var repoID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT repo_id FROM packages
		WHERE type = 'container' AND name = $3 AND repo_id IS NOT NULL
		  AND (($1 <> 0 AND owner_user_id = $1) OR ($2 <> 0 AND owner_org_id = $2))`, userID, orgID, name).Scan(&repoID)
	if err != nil {
		return 0, fmt.Errorf("package linked repo: %w", err)
	}
	return repoID, nil
}

// HasPending reports whether creatorID holds an unexpired pending container package at the path.
func (s *PackageStore) HasPending(ctx context.Context, userID, orgID int64, name string, creatorID int64, now time.Time) (bool, error) {
	var held bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM packages
		  WHERE type = 'container' AND name = $3 AND repo_id IS NULL AND creator_id = $4 AND pending_expires_at > $5
		    AND (($1 <> 0 AND owner_user_id = $1) OR ($2 <> 0 AND owner_org_id = $2)))`,
		userID, orgID, name, creatorID, now).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("package pending: %w", err)
	}
	return held, nil
}
