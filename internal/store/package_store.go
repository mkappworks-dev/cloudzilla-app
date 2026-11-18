package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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

var (
	ErrPackageNameTaken  = errors.New("package name is linked to another repository")
	ErrPackageFileExists = errors.New("package file already exists")
)

const ownerMatch = `(($1::bigint <> 0 AND owner_user_id = $1) OR ($2::bigint <> 0 AND owner_org_id = $2))`

// GenericRepoID returns the repository the generic package at the path is linked to, or sql.ErrNoRows.
func (s *PackageStore) GenericRepoID(ctx context.Context, userID, orgID int64, name string) (int64, error) {
	var repoID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT repo_id FROM packages
		WHERE type = 'generic' AND name = $3 AND repo_id IS NOT NULL AND `+ownerMatch, userID, orgID, name).Scan(&repoID)
	if err != nil {
		return 0, fmt.Errorf("generic package repo: %w", err)
	}
	return repoID, nil
}

func (s *PackageStore) GenericFileExists(ctx context.Context, repoID int64, name, version, filename string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM package_files f
		  JOIN package_versions v ON v.id = f.version_id JOIN packages p ON p.id = v.package_id
		  WHERE p.type = 'generic' AND p.repo_id = $1 AND p.name = $2 AND v.version = $3 AND f.filename = $4)`,
		repoID, name, version, filename).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("generic file exists: %w", err)
	}
	return exists, nil
}

// AddGenericFile creates the package and version on first use and adds f, all in one transaction so a refused
// upload leaves no empty package behind. It returns ErrPackageNameTaken when the path owner already links the name
// to another repository, and ErrPackageFileExists when the file is there.
func (s *PackageStore) AddGenericFile(ctx context.Context, userID, orgID, repoID, creatorID int64, name, version string, f *model.PackageFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add generic file: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO packages (owner_user_id, owner_org_id, type, name, repo_id, creator_id)
		VALUES (NULLIF($1::bigint, 0), NULLIF($2::bigint, 0), 'generic', $3, $4, $5)
		ON CONFLICT DO NOTHING`, userID, orgID, name, repoID, creatorID); err != nil {
		return fmt.Errorf("add generic file: create package: %w", err)
	}
	var packageID, linked int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, repo_id FROM packages
		WHERE type = 'generic' AND name = $3 AND repo_id IS NOT NULL AND `+ownerMatch, userID, orgID, name).Scan(&packageID, &linked)
	if err != nil {
		return fmt.Errorf("add generic file: find package: %w", err)
	}
	if linked != repoID {
		return ErrPackageNameTaken
	}

	var versionID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO package_versions (package_id, version) VALUES ($1, $2)
		ON CONFLICT (package_id, version) DO UPDATE SET version = EXCLUDED.version
		RETURNING id`, packageID, version).Scan(&versionID)
	if err != nil {
		return fmt.Errorf("add generic file: version: %w", err)
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO package_files (version_id, filename, size, sha256, storage_key, uploader_id)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		versionID, f.Filename, f.Size, f.SHA256, f.StorageKey, f.UploaderID).Scan(&f.ID, &f.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrPackageFileExists
	}
	if err != nil {
		return fmt.Errorf("add generic file: file: %w", err)
	}
	f.VersionID = versionID
	if _, err := tx.ExecContext(ctx, `UPDATE packages SET updated_at = NOW() WHERE id = $1`, packageID); err != nil {
		return fmt.Errorf("add generic file: touch package: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add generic file: %w", err)
	}
	return nil
}

// GenericFile returns the file at the path within the repository's package, or sql.ErrNoRows.
func (s *PackageStore) GenericFile(ctx context.Context, repoID int64, name, version, filename string) (*model.PackageFile, error) {
	var f model.PackageFile
	err := s.db.QueryRowContext(ctx, `
		SELECT f.id, f.version_id, f.filename, f.size, f.sha256, f.storage_key, f.uploader_id, f.created_at
		FROM package_files f
		JOIN package_versions v ON v.id = f.version_id JOIN packages p ON p.id = v.package_id
		WHERE p.type = 'generic' AND p.repo_id = $1 AND p.name = $2 AND v.version = $3 AND f.filename = $4`,
		repoID, name, version, filename).Scan(&f.ID, &f.VersionID, &f.Filename, &f.Size, &f.SHA256, &f.StorageKey, &f.UploaderID, &f.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("generic file: %w", err)
	}
	return &f, nil
}

// ListGeneric returns the repository's generic packages, most recently updated first.
func (s *PackageStore) ListGeneric(ctx context.Context, repoID int64) ([]model.GenericPackageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.created_at, p.updated_at,
		  COALESCE((SELECT v.version FROM package_versions v WHERE v.package_id = p.id ORDER BY v.created_at DESC, v.id DESC LIMIT 1), ''),
		  (SELECT COUNT(*) FROM package_versions v WHERE v.package_id = p.id),
		  (SELECT COALESCE(SUM(f.size), 0) FROM package_files f JOIN package_versions v ON v.id = f.version_id WHERE v.package_id = p.id)
		FROM packages p WHERE p.type = 'generic' AND p.repo_id = $1
		ORDER BY p.updated_at DESC, p.id DESC`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list generic packages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.GenericPackageSummary
	for rows.Next() {
		g := model.GenericPackageSummary{Package: model.Package{Type: model.PackageTypeGeneric, RepoID: &repoID}}
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatedAt, &g.UpdatedAt, &g.LatestVersion, &g.VersionCount, &g.TotalSize); err != nil {
			return nil, fmt.Errorf("list generic packages: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GenericVersions returns the versions of the repository's generic package, newest first, each with its files by
// name, or sql.ErrNoRows when there is no such package.
func (s *PackageStore) GenericVersions(ctx context.Context, repoID int64, name string) (*model.Package, []model.GenericVersionFiles, error) {
	p := model.Package{Type: model.PackageTypeGeneric, Name: name, RepoID: &repoID}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, updated_at FROM packages WHERE type = 'generic' AND repo_id = $1 AND name = $2`,
		repoID, name).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("generic package: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.id, v.version, v.created_at, f.id, f.filename, f.size, f.sha256, f.storage_key, f.uploader_id, f.created_at
		FROM package_versions v LEFT JOIN package_files f ON f.version_id = v.id
		WHERE v.package_id = $1
		ORDER BY v.created_at DESC, v.id DESC, f.filename`, p.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("generic versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.GenericVersionFiles
	for rows.Next() {
		var v model.PackageVersion
		var fileID, size, uploader sql.NullInt64
		var filename, sum, key sql.NullString
		var fileCreated sql.NullTime
		if err := rows.Scan(&v.ID, &v.Version, &v.CreatedAt, &fileID, &filename, &size, &sum, &key, &uploader, &fileCreated); err != nil {
			return nil, nil, fmt.Errorf("generic versions: %w", err)
		}
		v.PackageID = p.ID
		if len(out) == 0 || out[len(out)-1].ID != v.ID {
			out = append(out, model.GenericVersionFiles{PackageVersion: v})
		}
		if fileID.Valid {
			f := model.PackageFile{ID: fileID.Int64, VersionID: v.ID, Filename: filename.String, Size: size.Int64,
				SHA256: sum.String, StorageKey: key.String, CreatedAt: fileCreated.Time}
			if uploader.Valid {
				f.UploaderID = &uploader.Int64
			}
			last := &out[len(out)-1]
			last.Files = append(last.Files, f)
		}
	}
	return &p, out, rows.Err()
}

// DeleteGenericFile removes one file row and returns its storage key. A version left with no files and a package
// left with no versions go with it, so the name is free again.
func (s *PackageStore) DeleteGenericFile(ctx context.Context, repoID int64, name, version, filename string) (string, error) {
	var key string
	err := s.deleteGeneric(ctx, repoID, name, version, func(tx *sql.Tx, versionID int64) error {
		return tx.QueryRowContext(ctx, `DELETE FROM package_files WHERE version_id = $1 AND filename = $2 RETURNING storage_key`,
			versionID, filename).Scan(&key)
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// DeleteGenericVersion removes a version with all its files and returns their storage keys.
func (s *PackageStore) DeleteGenericVersion(ctx context.Context, repoID int64, name, version string) ([]string, error) {
	var keys []string
	err := s.deleteGeneric(ctx, repoID, name, version, func(tx *sql.Tx, versionID int64) error {
		rows, err := tx.QueryContext(ctx, `DELETE FROM package_files WHERE version_id = $1 RETURNING storage_key`, versionID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *PackageStore) deleteGeneric(ctx context.Context, repoID int64, name, version string, remove func(tx *sql.Tx, versionID int64) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete generic: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var packageID, versionID int64
	err = tx.QueryRowContext(ctx, `
		SELECT p.id, v.id FROM packages p JOIN package_versions v ON v.package_id = p.id
		WHERE p.type = 'generic' AND p.repo_id = $1 AND p.name = $2 AND v.version = $3 FOR UPDATE OF p, v`,
		repoID, name, version).Scan(&packageID, &versionID)
	if err != nil {
		return fmt.Errorf("delete generic: %w", err)
	}
	if err := remove(tx, versionID); err != nil {
		return fmt.Errorf("delete generic: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM package_versions WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM package_files WHERE version_id = $1)`, versionID); err != nil {
		return fmt.Errorf("delete generic: prune version: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM packages WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM package_versions WHERE package_id = $1)`, packageID); err != nil {
		return fmt.Errorf("delete generic: prune package: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE packages SET updated_at = NOW() WHERE id = $1`, packageID); err != nil {
		return fmt.Errorf("delete generic: touch package: %w", err)
	}
	return tx.Commit()
}
