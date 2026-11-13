package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// The lookups below serve a whole page of commits at once: a constant number of queries, never one per commit.

// ListByKeyIDs returns every key whose primary or subkey long ID is in keyIDs, with subkeys and UID emails, in three
// queries however many IDs there are.
func (s *GPGKeyStore) ListByKeyIDs(ctx context.Context, keyIDs []string) ([]model.GPGKey, error) {
	if len(keyIDs) == 0 {
		return nil, nil
	}
	ph, args := textPlaceholders(1, keyIDs)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, key_id, fingerprint, public_key, expires_at, revoked_at, created_at
		 FROM gpg_keys
		 WHERE key_id IN (`+ph+`) OR id IN (SELECT gpg_key_id FROM gpg_subkeys WHERE key_id IN (`+ph+`))
		 ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("gpg keys by key id: %w", err)
	}
	defer rows.Close()
	var keys []model.GPGKey
	var ids []int64
	for rows.Next() {
		var k model.GPGKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyID, &k.Fingerprint, &k.PublicKey, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		k.Subkeys, k.Emails = []model.GPGSubkey{}, []string{}
		keys = append(keys, k)
		ids = append(ids, k.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	byID := map[int64]*model.GPGKey{}
	for i := range keys {
		byID[keys[i].ID] = &keys[i]
	}

	idPH, idArgs := inPlaceholders(1, ids)
	subs, err := s.db.QueryContext(ctx,
		`SELECT id, gpg_key_id, key_id, fingerprint, can_sign, expires_at, revoked_at
		 FROM gpg_subkeys WHERE gpg_key_id IN (`+idPH+`) ORDER BY id`, idArgs...)
	if err != nil {
		return nil, fmt.Errorf("gpg subkeys by key: %w", err)
	}
	defer subs.Close()
	for subs.Next() {
		var sk model.GPGSubkey
		if err := subs.Scan(&sk.ID, &sk.GPGKeyID, &sk.KeyID, &sk.Fingerprint, &sk.CanSign, &sk.ExpiresAt, &sk.RevokedAt); err != nil {
			return nil, err
		}
		byID[sk.GPGKeyID].Subkeys = append(byID[sk.GPGKeyID].Subkeys, sk)
	}
	if err := subs.Err(); err != nil {
		return nil, err
	}

	emails, err := s.db.QueryContext(ctx,
		`SELECT gpg_key_id, email FROM gpg_key_emails WHERE gpg_key_id IN (`+idPH+`) ORDER BY email`, idArgs...)
	if err != nil {
		return nil, fmt.Errorf("gpg key emails by key: %w", err)
	}
	defer emails.Close()
	for emails.Next() {
		var id int64
		var email string
		if err := emails.Scan(&id, &email); err != nil {
			return nil, err
		}
		byID[id].Emails = append(byID[id].Emails, email)
	}
	return keys, emails.Err()
}

// ListByFingerprints returns the keys with any of the SHA256 fingerprints, in one query.
func (s *SSHSigningKeyStore) ListByFingerprints(ctx context.Context, fingerprints []string) ([]model.SSHSigningKey, error) {
	if len(fingerprints) == 0 {
		return nil, nil
	}
	ph, args := textPlaceholders(1, fingerprints)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, title, public_key, fingerprint, created_at
		 FROM ssh_signing_keys WHERE fingerprint IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("ssh signing keys by fingerprint: %w", err)
	}
	defer rows.Close()
	var keys []model.SSHSigningKey
	for rows.Next() {
		var k model.SSHSigningKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Title, &k.PublicKey, &k.Fingerprint, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// textPlaceholders is inPlaceholders for strings.
func textPlaceholders(start int, vals []string) (string, []any) {
	ph := make([]string, len(vals))
	args := make([]any, len(vals))
	for i, v := range vals {
		ph[i] = fmt.Sprintf("$%d", start+i)
		args[i] = v
	}
	return strings.Join(ph, ","), args
}
