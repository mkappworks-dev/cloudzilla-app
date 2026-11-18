package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DeleteUnreferencedLinks removes up to limit link rows created before cutoff that no manifest of the package
// names, and reports how many went.
func (s *RegistryStore) DeleteUnreferencedLinks(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM package_blobs WHERE (package_id, digest) IN (
		SELECT pb.package_id, pb.digest FROM package_blobs pb
		WHERE pb.created_at < $1 AND NOT EXISTS (
		  SELECT 1 FROM package_manifest_blobs mb JOIN package_manifests m ON m.id = mb.manifest_id
		  WHERE m.package_id = pb.package_id AND mb.digest = pb.digest)
		LIMIT $2)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("delete unreferenced links: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteExpiredPendingPackages removes up to limit pending packages that expired by now, with their rows by
// cascade, and returns the spool files of their upload sessions.
func (s *RegistryStore) DeleteExpiredPendingPackages(ctx context.Context, now time.Time, limit int) ([]string, int, error) {
	return s.spoolPaths(ctx, `
		WITH gone AS (
		  DELETE FROM packages WHERE id IN (
		    SELECT id FROM packages WHERE repo_id IS NULL AND pending_expires_at <= $1 LIMIT $2)
		  RETURNING id)
		SELECT u.temp_path, (SELECT COUNT(*) FROM gone)
		FROM gone g LEFT JOIN package_upload_sessions u ON u.package_id = g.id`, now, limit)
}

// DeleteExpiredSessions removes up to limit sessions started before cutoff and returns their spool files.
func (s *RegistryStore) DeleteExpiredSessions(ctx context.Context, cutoff time.Time, limit int) ([]string, int, error) {
	return s.spoolPaths(ctx, `
		WITH gone AS (
		  DELETE FROM package_upload_sessions WHERE id IN (
		    SELECT id FROM package_upload_sessions WHERE created_at < $1 LIMIT $2)
		  RETURNING temp_path)
		SELECT temp_path, (SELECT COUNT(*) FROM gone) FROM gone`, cutoff, limit)
}

// spoolPaths runs a query that selects (temp_path or NULL, count of deleted rows).
func (s *RegistryStore) spoolPaths(ctx context.Context, query string, args ...any) ([]string, int, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("delete expired uploads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var paths []string
	count := 0
	for rows.Next() {
		var p sql.NullString
		if err := rows.Scan(&p, &count); err != nil {
			return nil, 0, fmt.Errorf("scan spool path: %w", err)
		}
		if p.Valid {
			paths = append(paths, p.String)
		}
	}
	return paths, count, rows.Err()
}

// MarkUnlinkedBlobs stamps up to limit blobs that have no link and no stamp yet, and reports how many.
func (s *RegistryStore) MarkUnlinkedBlobs(ctx context.Context, now time.Time, limit int) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE registry_blobs SET unlinked_at = $1 WHERE digest IN (
		SELECT b.digest FROM registry_blobs b WHERE b.unlinked_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM package_blobs pb WHERE pb.digest = b.digest) LIMIT $2)`, now, limit)
	if err != nil {
		return 0, fmt.Errorf("mark unlinked blobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// UnlinkedBlobsAfter lists up to limit digests above after whose last link went before cutoff.
func (s *RegistryStore) UnlinkedBlobsAfter(ctx context.Context, cutoff time.Time, after string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT digest FROM registry_blobs
		WHERE unlinked_at IS NOT NULL AND unlinked_at <= $1 AND digest > $2 ORDER BY digest LIMIT $3`, cutoff, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list unlinked blobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("scan blob digest: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ReapBlob deletes the blob's object through deleteObject and then its row, in one transaction holding the row
// lock, so a concurrent link (FOR SHARE) waits and then finds no row. A blob someone locked, or linked again since
// it was stamped, is left alone and reported as not reaped. A failed deleteObject rolls the row back.
func (s *RegistryStore) ReapBlob(ctx context.Context, digest string, cutoff time.Time, deleteObject func(context.Context) error) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin reap blob: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var held string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM registry_blobs
		WHERE digest = $1 AND unlinked_at IS NOT NULL AND unlinked_at <= $2 FOR UPDATE SKIP LOCKED`, digest, cutoff).Scan(&held)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock blob: %w", err)
	}
	var linked bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM package_blobs WHERE digest = $1)`, digest).Scan(&linked); err != nil {
		return false, fmt.Errorf("recheck blob links: %w", err)
	}
	if linked {
		if _, err := tx.ExecContext(ctx, `UPDATE registry_blobs SET unlinked_at = NULL WHERE digest = $1`, digest); err != nil {
			return false, fmt.Errorf("clear unlinked_at: %w", err)
		}
		return false, tx.Commit()
	}
	if err := deleteObject(ctx); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM registry_blobs WHERE digest = $1`, digest); err != nil {
		return false, fmt.Errorf("delete blob row: %w", err)
	}
	return true, tx.Commit()
}

// TagPulls is a batch of downloads of one tag.
type TagPulls struct {
	PackageID int64
	Name      string
	Count     int64
}

// AddPullCounts adds the batch to the manifests' and tags' counters in one transaction. A tag that moved or went
// away since the pull is skipped by the UPDATE matching nothing.
func (s *RegistryStore) AddPullCounts(ctx context.Context, manifests map[int64]int64, tags []TagPulls) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin pull counts: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for id, n := range manifests {
		if _, err := tx.ExecContext(ctx, `UPDATE package_manifests SET pull_count = pull_count + $2 WHERE id = $1`, id, n); err != nil {
			return fmt.Errorf("add manifest pulls: %w", err)
		}
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, `UPDATE package_tags SET pull_count = pull_count + $3 WHERE package_id = $1 AND name = $2`,
			t.PackageID, t.Name, t.Count); err != nil {
			return fmt.Errorf("add tag pulls: %w", err)
		}
	}
	return tx.Commit()
}
