package store

import (
	"context"
	"database/sql"
	"fmt"
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
