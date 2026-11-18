package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// packageSize is the bytes a package row shows: its distinct linked blobs, or its generic files.
const packageSize = `CASE p.type
	WHEN 'container' THEN (SELECT COALESCE(SUM(b.size), 0) FROM package_blobs pb JOIN registry_blobs b ON b.digest = pb.digest WHERE pb.package_id = p.id)
	ELSE (SELECT COALESCE(SUM(f.size), 0) FROM package_files f JOIN package_versions v ON v.id = f.version_id WHERE v.package_id = p.id)
END`

// manifestSize is the distinct blob bytes of manifest m, adding those of every child when m is an index.
const manifestSize = `COALESCE((SELECT SUM(b.size) FROM registry_blobs b WHERE b.digest IN (
	SELECT mb.digest FROM package_manifest_blobs mb WHERE mb.manifest_id = m.id
	UNION
	SELECT cb.digest FROM package_manifest_children c
	  JOIN package_manifests cm ON cm.package_id = m.package_id AND cm.digest = c.child_digest
	  JOIN package_manifest_blobs cb ON cb.manifest_id = cm.id
	WHERE c.manifest_id = m.id)), 0)`

func (s *PackageStore) listSummaries(ctx context.Context, where string, args ...any) ([]model.PackageSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.owner_user_id, p.owner_org_id, p.type, p.name, p.repo_id, p.created_at, p.updated_at, r.owner_name, r.name,
		  COALESCE(u.username, o.name, ''),
		  `+packageSize+`,
		  (SELECT COUNT(*) FROM package_tags t WHERE t.package_id = p.id),
		  (SELECT COUNT(*) FROM package_versions v WHERE v.package_id = p.id),
		  CASE p.type
		    WHEN 'container' THEN COALESCE((SELECT t.name FROM package_tags t WHERE t.package_id = p.id ORDER BY t.updated_at DESC, t.id DESC LIMIT 1), '')
		    ELSE COALESCE((SELECT v.version FROM package_versions v WHERE v.package_id = p.id ORDER BY v.created_at DESC, v.id DESC LIMIT 1), '')
		  END,
		  (SELECT COALESCE(SUM(m.pull_count), 0) FROM package_manifests m WHERE m.package_id = p.id)
		FROM packages p JOIN repositories r ON r.id = p.repo_id AND r.deleted_at IS NULL
		  LEFT JOIN users u ON u.id = p.owner_user_id LEFT JOIN organizations o ON o.id = p.owner_org_id
		WHERE `+where+`
		ORDER BY p.updated_at DESC, p.id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.PackageSummary
	for rows.Next() {
		var p model.PackageSummary
		var repoID int64
		var userID, orgID sql.NullInt64
		if err := rows.Scan(&p.ID, &userID, &orgID, &p.Type, &p.Name, &repoID, &p.CreatedAt, &p.UpdatedAt, &p.RepoOwner, &p.RepoName,
			&p.OwnerName, &p.Size, &p.TagCount, &p.VersionCount, &p.LatestVersion, &p.Pulls); err != nil {
			return nil, fmt.Errorf("scan package: %w", err)
		}
		p.RepoID = &repoID
		if userID.Valid {
			p.OwnerUserID = &userID.Int64
		}
		if orgID.Valid {
			p.OwnerOrgID = &orgID.Int64
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListByRepo returns the packages linked to repoID. A pending package has no Repository, so it never appears.
func (s *PackageStore) ListByRepo(ctx context.Context, repoID int64) ([]model.PackageSummary, error) {
	return s.listSummaries(ctx, `p.repo_id = $1`, repoID)
}

// CountByRepo counts the packages linked to repoID.
func (s *PackageStore) CountByRepo(ctx context.Context, repoID int64) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM packages WHERE repo_id = $1`, repoID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count packages: %w", err)
	}
	return n, nil
}

// ListByPathOwner returns the linked packages under a path owner, whatever Repository they now sit in.
func (s *PackageStore) ListByPathOwner(ctx context.Context, userID, orgID int64) ([]model.PackageSummary, error) {
	col, id := "owner_user_id", userID
	if orgID != 0 {
		col, id = "owner_org_id", orgID
	}
	return s.listSummaries(ctx, `p.`+col+` = $1`, id)
}

// PackageByID returns sql.ErrNoRows for an unknown package.
func (s *PackageStore) PackageByID(ctx context.Context, id int64) (*model.Package, error) {
	return s.onePackage(ctx, `p.id = $1`, id)
}

// LinkedPackage returns the linked package of the type at the path, or sql.ErrNoRows.
func (s *PackageStore) LinkedPackage(ctx context.Context, userID, orgID int64, typ, name string) (*model.Package, error) {
	return s.onePackage(ctx, `p.type = $3 AND p.name = $4 AND p.repo_id IS NOT NULL
		AND (($1::bigint <> 0 AND p.owner_user_id = $1) OR ($2::bigint <> 0 AND p.owner_org_id = $2))`, userID, orgID, typ, name)
}

func (s *PackageStore) onePackage(ctx context.Context, where string, args ...any) (*model.Package, error) {
	var p model.Package
	var userID, orgID, repoID, creatorID sql.NullInt64
	var pending sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT p.id, p.owner_user_id, p.owner_org_id, p.type, p.name, p.repo_id, p.creator_id,
		p.pending_expires_at, p.created_at, p.updated_at FROM packages p WHERE `+where, args...).
		Scan(&p.ID, &userID, &orgID, &p.Type, &p.Name, &repoID, &creatorID, &pending, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("package: %w", err)
	}
	if userID.Valid {
		p.OwnerUserID = &userID.Int64
	}
	if orgID.Valid {
		p.OwnerOrgID = &orgID.Int64
	}
	if repoID.Valid {
		p.RepoID = &repoID.Int64
	}
	if creatorID.Valid {
		p.CreatorID = &creatorID.Int64
	}
	if pending.Valid {
		p.PendingExpiresAt = &pending.Time
	}
	return &p, nil
}

func splitPlatforms(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// Tags lists the package's tags by name with the manifest each points at.
func (s *PackageStore) Tags(ctx context.Context, packageID int64) ([]model.TagRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.name, m.digest, m.media_type, `+manifestSize+`, m.platforms, COALESCE(u.username, ''), t.pull_count, t.updated_at
		FROM package_tags t JOIN package_manifests m ON m.id = t.manifest_id LEFT JOIN users u ON u.id = t.pusher_id
		WHERE t.package_id = $1 ORDER BY t.name COLLATE "C"`, packageID)
	if err != nil {
		return nil, fmt.Errorf("list tag rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.TagRow
	for rows.Next() {
		var t model.TagRow
		var platforms string
		if err := rows.Scan(&t.Name, &t.Digest, &t.MediaType, &t.Size, &platforms, &t.Publisher, &t.PullCount, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tag row: %w", err)
		}
		t.Platforms = splitPlatforms(platforms)
		out = append(out, t)
	}
	return out, rows.Err()
}

// UntaggedManifests lists the manifests no tag points at, newest first. A manifest with a subject is a referrer
// (a signature, an SBOM), reachable through its subject, so it is left out.
func (s *PackageStore) UntaggedManifests(ctx context.Context, packageID int64) ([]model.ManifestRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.digest, m.media_type, `+manifestSize+`, COALESCE(m.artifact_type, ''), m.platforms, m.pull_count, m.created_at
		FROM package_manifests m
		WHERE m.package_id = $1 AND m.subject_digest IS NULL AND NOT EXISTS (SELECT 1 FROM package_tags t WHERE t.manifest_id = m.id)
		ORDER BY m.created_at DESC, m.id DESC`, packageID)
	if err != nil {
		return nil, fmt.Errorf("list untagged manifests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.ManifestRow
	for rows.Next() {
		var m model.ManifestRow
		var platforms string
		if err := rows.Scan(&m.Digest, &m.MediaType, &m.Size, &m.ArtifactType, &platforms, &m.PullCount, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan untagged manifest: %w", err)
		}
		m.Platforms = splitPlatforms(platforms)
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeletePackage removes the package row, which cascades to its links, manifests, tags, sessions, versions and files,
// and returns the storage keys of its generic files for the caller to delete. Registry blobs are left to the sweep.
func (s *PackageStore) DeletePackage(ctx context.Context, id int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("delete package: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT f.storage_key FROM package_files f JOIN package_versions v ON v.id = f.version_id WHERE v.package_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("delete package: list files: %w", err)
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("delete package: scan file: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("delete package: list files: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM packages WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("delete package: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	return keys, tx.Commit()
}

// RelinkPackage points a linked package at another Repository; it returns sql.ErrNoRows for a pending or missing one.
func (s *PackageStore) RelinkPackage(ctx context.Context, id, repoID int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE packages SET repo_id = $2, updated_at = NOW() WHERE id = $1 AND repo_id IS NOT NULL`, id, repoID)
	if err != nil {
		return fmt.Errorf("relink package: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
