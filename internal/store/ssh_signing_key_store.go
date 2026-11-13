package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// SSHSigningKeyStore provides database operations for SSH keys that sign commits.
type SSHSigningKeyStore struct{ db *sql.DB }

func NewSSHSigningKeyStore(db *sql.DB) *SSHSigningKeyStore { return &SSHSigningKeyStore{db: db} }

const sshSigningKeyColumns = `id, user_id, title, public_key, fingerprint, removed_at, revoked_at, created_at`

func scanSSHSigningKey(r interface{ Scan(...any) error }) (model.SSHSigningKey, error) {
	var k model.SSHSigningKey
	err := r.Scan(&k.ID, &k.UserID, &k.Title, &k.PublicKey, &k.Fingerprint, &k.RemovedAt, &k.RevokedAt, &k.CreatedAt)
	k.Status = model.SigningKeyStatus(k.RemovedAt, k.RevokedAt)
	return k, err
}

func (s *SSHSigningKeyStore) Create(ctx context.Context, key *model.SSHSigningKey) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ssh_signing_keys (user_id, title, public_key, fingerprint)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		key.UserID, key.Title, key.PublicKey, key.Fingerprint,
	).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return signingKeyInsertError("ssh signing key create", err)
	}
	key.Status = model.SigningKeyActive
	return nil
}

// FindByFingerprint returns the key with that SHA256 fingerprint whoever owns it, or nil.
func (s *SSHSigningKeyStore) FindByFingerprint(ctx context.Context, fingerprint string) (*model.SSHSigningKey, error) {
	k, err := scanSSHSigningKey(s.db.QueryRowContext(ctx,
		`SELECT `+sshSigningKeyColumns+` FROM ssh_signing_keys WHERE fingerprint = $1`, fingerprint))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ssh signing key by fingerprint: %w", err)
	}
	return &k, nil
}

// ListByUser returns userID's keys, newest first. Removed keys come back only with includeRemoved; a revoked key
// always does.
func (s *SSHSigningKeyStore) ListByUser(ctx context.Context, userID int64, includeRemoved bool) ([]model.SSHSigningKey, error) {
	filter := ""
	if !includeRemoved {
		filter = ` AND (removed_at IS NULL OR revoked_at IS NOT NULL)`
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sshSigningKeyColumns+` FROM ssh_signing_keys WHERE user_id = $1`+filter+` ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("ssh signing keys list: %w", err)
	}
	defer rows.Close()
	var keys []model.SSHSigningKey
	for rows.Next() {
		k, err := scanSSHSigningKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Remove soft-deletes userID's key from now on; false means no such key of theirs. Removing twice keeps the first date.
func (s *SSHSigningKeyStore) Remove(ctx context.Context, keyID, userID int64) (bool, error) {
	return execOwned(ctx, s.db, "ssh signing key remove",
		`UPDATE ssh_signing_keys SET removed_at = COALESCE(removed_at, NOW()) WHERE id = $1 AND user_id = $2`, keyID, userID)
}

// Revoke marks userID's key compromised; false means no such key of theirs.
func (s *SSHSigningKeyStore) Revoke(ctx context.Context, keyID, userID int64) (bool, error) {
	return execOwned(ctx, s.db, "ssh signing key revoke",
		`UPDATE ssh_signing_keys SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1 AND user_id = $2`, keyID, userID)
}

// Restore re-activates userID's removed key. found is false when userID has no such key; revoked when it is revoked
// and so stays as it is.
func (s *SSHSigningKeyStore) Restore(ctx context.Context, keyID, userID int64) (found, revoked bool, err error) {
	ok, err := execOwned(ctx, s.db, "ssh signing key restore",
		`UPDATE ssh_signing_keys SET removed_at = NULL WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, keyID, userID)
	if err != nil || ok {
		return ok, false, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT revoked_at IS NOT NULL FROM ssh_signing_keys WHERE id = $1 AND user_id = $2`, keyID, userID).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return err == nil, revoked, err
}
