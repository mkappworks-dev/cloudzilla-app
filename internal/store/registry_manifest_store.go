package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ManifestRecord is a manifest as stored: the exact bytes, and the blobs and children it names.
type ManifestRecord struct {
	Digest       string
	MediaType    string
	Content      []byte
	Subject      string
	ArtifactType string
	Platforms    string
	Blobs        []string
	Children     []string
}

// StoredManifest is a manifest read back for a pull.
type StoredManifest struct {
	ID        int64
	Digest    string
	MediaType string
	Content   []byte
	Platforms string
}

func inClause(start int, n int) string {
	ph := make([]string, n)
	for i := range ph {
		ph[i] = "$" + strconv.Itoa(start+i)
	}
	return strings.Join(ph, ",")
}

func digestArgs(first []any, digests []string) []any {
	args := first
	for _, d := range digests {
		args = append(args, d)
	}
	return args
}

// LinkedBlobs returns those of digests the package links.
func (s *RegistryStore) LinkedBlobs(ctx context.Context, packageID int64, digests []string) (map[string]bool, error) {
	return s.digestSet(ctx, `SELECT digest FROM package_blobs WHERE package_id = $1 AND digest IN (`, packageID, digests)
}

// ManifestDigests returns those of digests that are manifests of the package.
func (s *RegistryStore) ManifestDigests(ctx context.Context, packageID int64, digests []string) (map[string]bool, error) {
	return s.digestSet(ctx, `SELECT digest FROM package_manifests WHERE package_id = $1 AND digest IN (`, packageID, digests)
}

func (s *RegistryStore) digestSet(ctx context.Context, prefix string, packageID int64, digests []string) (map[string]bool, error) {
	found := map[string]bool{}
	if len(digests) == 0 {
		return found, nil
	}
	rows, err := s.db.QueryContext(ctx, prefix+inClause(2, len(digests))+`)`, digestArgs([]any{packageID}, digests)...)
	if err != nil {
		return nil, fmt.Errorf("package digests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("scan digest: %w", err)
		}
		found[d] = true
	}
	return found, rows.Err()
}

// PutManifest stores the manifest with its reference rows and, when tag is not empty, points the tag at it. A
// manifest already stored under the digest is kept as it is.
func (s *RegistryStore) PutManifest(ctx context.Context, packageID int64, m ManifestRecord, tag string, pusherID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin put manifest: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	var inserted bool
	err = tx.QueryRowContext(ctx, `INSERT INTO package_manifests (package_id, digest, media_type, size, content, subject_digest, artifact_type, platforms)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
		ON CONFLICT (package_id, digest) DO UPDATE SET digest = EXCLUDED.digest
		RETURNING id, (xmax = 0)`, packageID, m.Digest, m.MediaType, len(m.Content), m.Content, m.Subject, m.ArtifactType, m.Platforms).Scan(&id, &inserted)
	if err != nil {
		return fmt.Errorf("insert manifest: %w", err)
	}
	if inserted {
		for _, d := range m.Blobs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO package_manifest_blobs (manifest_id, digest) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, d); err != nil {
				return fmt.Errorf("insert manifest blob: %w", err)
			}
		}
		for _, d := range m.Children {
			if _, err := tx.ExecContext(ctx, `INSERT INTO package_manifest_children (manifest_id, child_digest) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, d); err != nil {
				return fmt.Errorf("insert manifest child: %w", err)
			}
		}
	}
	if tag != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO package_tags (package_id, name, manifest_id, pusher_id) VALUES ($1, $2, $3, $4)
			ON CONFLICT (package_id, name) DO UPDATE SET
			  pull_count = CASE WHEN package_tags.manifest_id = EXCLUDED.manifest_id THEN package_tags.pull_count ELSE 0 END,
			  manifest_id = EXCLUDED.manifest_id, pusher_id = EXCLUDED.pusher_id, updated_at = NOW()`,
			packageID, tag, id, pusherID); err != nil {
			return fmt.Errorf("upsert tag: %w", err)
		}
	}
	return tx.Commit()
}

// ManifestByDigest returns the package's manifest at digest, or sql.ErrNoRows.
func (s *RegistryStore) ManifestByDigest(ctx context.Context, packageID int64, digest string) (*StoredManifest, error) {
	var m StoredManifest
	err := s.db.QueryRowContext(ctx, `SELECT id, digest, media_type, content, platforms FROM package_manifests WHERE package_id = $1 AND digest = $2`,
		packageID, digest).Scan(&m.ID, &m.Digest, &m.MediaType, &m.Content, &m.Platforms)
	if err != nil {
		return nil, fmt.Errorf("manifest by digest: %w", err)
	}
	return &m, nil
}

// ManifestByTag returns the manifest the package's tag points at, or sql.ErrNoRows.
func (s *RegistryStore) ManifestByTag(ctx context.Context, packageID int64, tag string) (*StoredManifest, error) {
	var m StoredManifest
	err := s.db.QueryRowContext(ctx, `SELECT m.id, m.digest, m.media_type, m.content, m.platforms FROM package_tags t
		JOIN package_manifests m ON m.id = t.manifest_id WHERE t.package_id = $1 AND t.name = $2`,
		packageID, tag).Scan(&m.ID, &m.Digest, &m.MediaType, &m.Content, &m.Platforms)
	if err != nil {
		return nil, fmt.Errorf("manifest by tag: %w", err)
	}
	return &m, nil
}

// IsManifestChild reports whether an index of the package names digest.
func (s *RegistryStore) IsManifestChild(ctx context.Context, packageID int64, digest string) (bool, error) {
	var held bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM package_manifest_children c
		JOIN package_manifests m ON m.id = c.manifest_id WHERE m.package_id = $1 AND c.child_digest = $2)`, packageID, digest).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("manifest child check: %w", err)
	}
	return held, nil
}

// DeleteManifest removes the manifest and, by cascade, its tags and reference rows. It reports false when the
// package has no such manifest.
func (s *RegistryStore) DeleteManifest(ctx context.Context, packageID int64, digest string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM package_manifests WHERE package_id = $1 AND digest = $2`, packageID, digest)
	if err != nil {
		return false, fmt.Errorf("delete manifest: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteTag removes the tag and leaves its manifest. It reports false when the package has no such tag.
func (s *RegistryStore) DeleteTag(ctx context.Context, packageID int64, name string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM package_tags WHERE package_id = $1 AND name = $2`, packageID, name)
	if err != nil {
		return false, fmt.Errorf("delete tag: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// BlobReferenced reports whether a manifest of the package names the blob.
func (s *RegistryStore) BlobReferenced(ctx context.Context, packageID int64, digest string) (bool, error) {
	var held bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM package_manifest_blobs b
		JOIN package_manifests m ON m.id = b.manifest_id WHERE m.package_id = $1 AND b.digest = $2)`, packageID, digest).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("blob reference check: %w", err)
	}
	return held, nil
}

// UnlinkBlob removes the package's link to the blob. It reports false when there was none.
func (s *RegistryStore) UnlinkBlob(ctx context.Context, packageID int64, digest string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM package_blobs WHERE package_id = $1 AND digest = $2`, packageID, digest)
	if err != nil {
		return false, fmt.Errorf("unlink blob: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// TagNames lists up to limit tag names after last in byte order; a negative limit lists them all.
func (s *RegistryStore) TagNames(ctx context.Context, packageID int64, last string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM package_tags
		WHERE package_id = $1 AND ($2 = '' OR name COLLATE "C" > $2 COLLATE "C")
		ORDER BY name COLLATE "C" LIMIT CASE WHEN $3 < 0 THEN NULL ELSE $3 END`, packageID, last, limit)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// ErrImageNameTaken is a name another package linked first.
var ErrImageNameTaken = errors.New("image name taken")

// LinkPackage links a pending package to a Repository and ends its pending expiry. It returns sql.ErrNoRows when
// the package is no longer pending and ErrImageNameTaken when another package holds the name by now.
func (s *RegistryStore) LinkPackage(ctx context.Context, packageID, repoID int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE packages SET repo_id = $2, pending_expires_at = NULL, updated_at = NOW()
		WHERE id = $1 AND repo_id IS NULL`, packageID, repoID)
	if isUniqueViolation(err) {
		return ErrImageNameTaken
	}
	if err != nil {
		return fmt.Errorf("link package: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Referrer is a manifest whose subject is the digest asked about.
type Referrer struct {
	Digest       string
	MediaType    string
	Size         int64
	ArtifactType string
}

// Referrers lists the package's manifests whose subject is subject, oldest first. A non-empty artifactType keeps
// only the manifests of that type.
func (s *RegistryStore) Referrers(ctx context.Context, packageID int64, subject, artifactType string) ([]Referrer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT digest, media_type, size, COALESCE(artifact_type, '') FROM package_manifests
		WHERE package_id = $1 AND subject_digest = $2 AND ($3 = '' OR artifact_type = $3)
		ORDER BY created_at, id`, packageID, subject, artifactType)
	if err != nil {
		return nil, fmt.Errorf("list referrers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Referrer{}
	for rows.Next() {
		var r Referrer
		if err := rows.Scan(&r.Digest, &r.MediaType, &r.Size, &r.ArtifactType); err != nil {
			return nil, fmt.Errorf("scan referrer: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
