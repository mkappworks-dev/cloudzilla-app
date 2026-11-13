package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ErrSigningKeyTaken means the key's fingerprint or key ID is already registered, by any account.
var ErrSigningKeyTaken = errors.New("signing key already registered")

// GPGKeyStore provides database operations for GPG signing keys, their subkeys and UID emails.
type GPGKeyStore struct{ db *sql.DB }

func NewGPGKeyStore(db *sql.DB) *GPGKeyStore { return &GPGKeyStore{db: db} }

const gpgKeyColumns = `id, user_id, key_id, fingerprint, public_key, expires_at, revoked_at, removed_at, created_at`

func scanGPGKey(r interface{ Scan(...any) error }) (model.GPGKey, error) {
	var k model.GPGKey
	err := r.Scan(&k.ID, &k.UserID, &k.KeyID, &k.Fingerprint, &k.PublicKey, &k.ExpiresAt, &k.RevokedAt, &k.RemovedAt, &k.CreatedAt)
	k.Status = model.SigningKeyStatus(k.RemovedAt, k.RevokedAt)
	k.Subkeys, k.Emails = []model.GPGSubkey{}, []string{}
	return k, err
}

func (s *GPGKeyStore) Create(ctx context.Context, key *model.GPGKey) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gpg key create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx,
		`INSERT INTO gpg_keys (user_id, key_id, fingerprint, public_key, expires_at, revoked_at, removed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		key.UserID, key.KeyID, key.Fingerprint, key.PublicKey, key.ExpiresAt, key.RevokedAt, key.RemovedAt,
	).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return signingKeyInsertError("gpg key create", err)
	}
	if err := insertGPGChildren(ctx, tx, key); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gpg key create: %w", err)
	}
	key.Status = model.SigningKeyStatus(key.RemovedAt, key.RevokedAt)
	return nil
}

func insertGPGChildren(ctx context.Context, tx *sql.Tx, key *model.GPGKey) error {
	for i := range key.Subkeys {
		sk := &key.Subkeys[i]
		sk.GPGKeyID = key.ID
		err := tx.QueryRowContext(ctx,
			`INSERT INTO gpg_subkeys (gpg_key_id, key_id, fingerprint, can_sign, expires_at, revoked_at, removed_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			key.ID, sk.KeyID, sk.Fingerprint, sk.CanSign, sk.ExpiresAt, sk.RevokedAt, sk.RemovedAt,
		).Scan(&sk.ID)
		if err != nil {
			return signingKeyInsertError("gpg subkey create", err)
		}
	}
	for _, email := range key.Emails {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gpg_key_emails (gpg_key_id, email) VALUES ($1, $2)`, key.ID, email); err != nil {
			return fmt.Errorf("gpg key email create: %w", err)
		}
	}
	return nil
}

// Update replaces the stored key whose ID is key.ID with key's content, subkeys and UID emails included. The caller
// merges the sticky states first.
func (s *GPGKeyStore) Update(ctx context.Context, key *model.GPGKey) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gpg key update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx,
		`UPDATE gpg_keys SET public_key = $2, expires_at = $3, revoked_at = $4, removed_at = $5
		 WHERE id = $1 RETURNING created_at`,
		key.ID, key.PublicKey, key.ExpiresAt, key.RevokedAt, key.RemovedAt,
	).Scan(&key.CreatedAt)
	if err != nil {
		return fmt.Errorf("gpg key update: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gpg_subkeys WHERE gpg_key_id = $1`, key.ID); err != nil {
		return fmt.Errorf("gpg subkeys replace: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gpg_key_emails WHERE gpg_key_id = $1`, key.ID); err != nil {
		return fmt.Errorf("gpg key emails replace: %w", err)
	}
	if err := insertGPGChildren(ctx, tx, key); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gpg key update: %w", err)
	}
	key.Status = model.SigningKeyStatus(key.RemovedAt, key.RevokedAt)
	return nil
}

func signingKeyInsertError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrSigningKeyTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}

// FindByFingerprint returns the key with that fingerprint whoever owns it, or nil.
func (s *GPGKeyStore) FindByFingerprint(ctx context.Context, fingerprint string) (*model.GPGKey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+gpgKeyColumns+` FROM gpg_keys WHERE fingerprint = $1`, fingerprint)
	k, err := scanGPGKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gpg key by fingerprint: %w", err)
	}
	keys := []model.GPGKey{k}
	if err := s.attachChildren(ctx, keys); err != nil {
		return nil, err
	}
	return &keys[0], nil
}

// ListByUser returns userID's keys, newest first. Removed keys come back only with includeRemoved; a revoked key
// always does.
func (s *GPGKeyStore) ListByUser(ctx context.Context, userID int64, includeRemoved bool) ([]model.GPGKey, error) {
	filter := ""
	if !includeRemoved {
		filter = ` AND (removed_at IS NULL OR revoked_at IS NOT NULL)`
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+gpgKeyColumns+` FROM gpg_keys WHERE user_id = $1`+filter+` ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("gpg keys list: %w", err)
	}
	defer rows.Close()
	var keys []model.GPGKey
	for rows.Next() {
		k, err := scanGPGKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, s.attachChildren(ctx, keys)
}

// ListByKeyIDs returns every key whose primary or subkey long ID is in keyIDs, removed and revoked ones included, with
// subkeys and UID emails, in three queries however many IDs there are.
func (s *GPGKeyStore) ListByKeyIDs(ctx context.Context, keyIDs []string) ([]model.GPGKey, error) {
	if len(keyIDs) == 0 {
		return nil, nil
	}
	ph, args := textPlaceholders(1, keyIDs)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+gpgKeyColumns+`
		 FROM gpg_keys
		 WHERE key_id IN (`+ph+`) OR id IN (SELECT gpg_key_id FROM gpg_subkeys WHERE key_id IN (`+ph+`))
		 ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("gpg keys by key id: %w", err)
	}
	defer rows.Close()
	var keys []model.GPGKey
	for rows.Next() {
		k, err := scanGPGKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	return keys, s.attachChildren(ctx, keys)
}

// attachChildren fills Subkeys and Emails of keys in two queries.
func (s *GPGKeyStore) attachChildren(ctx context.Context, keys []model.GPGKey) error {
	if len(keys) == 0 {
		return nil
	}
	byID := make(map[int64]*model.GPGKey, len(keys))
	ids := make([]int64, 0, len(keys))
	for i := range keys {
		byID[keys[i].ID] = &keys[i]
		ids = append(ids, keys[i].ID)
	}
	idPH, idArgs := inPlaceholders(1, ids)

	subs, err := s.db.QueryContext(ctx,
		`SELECT id, gpg_key_id, key_id, fingerprint, can_sign, expires_at, revoked_at, removed_at
		 FROM gpg_subkeys WHERE gpg_key_id IN (`+idPH+`) ORDER BY id`, idArgs...)
	if err != nil {
		return fmt.Errorf("gpg subkeys by key: %w", err)
	}
	defer subs.Close()
	for subs.Next() {
		var sk model.GPGSubkey
		if err := subs.Scan(&sk.ID, &sk.GPGKeyID, &sk.KeyID, &sk.Fingerprint, &sk.CanSign, &sk.ExpiresAt, &sk.RevokedAt, &sk.RemovedAt); err != nil {
			return err
		}
		byID[sk.GPGKeyID].Subkeys = append(byID[sk.GPGKeyID].Subkeys, sk)
	}
	if err := subs.Err(); err != nil {
		return err
	}

	emails, err := s.db.QueryContext(ctx,
		`SELECT gpg_key_id, email FROM gpg_key_emails WHERE gpg_key_id IN (`+idPH+`) ORDER BY email`, idArgs...)
	if err != nil {
		return fmt.Errorf("gpg key emails by key: %w", err)
	}
	defer emails.Close()
	for emails.Next() {
		var id int64
		var email string
		if err := emails.Scan(&id, &email); err != nil {
			return err
		}
		byID[id].Emails = append(byID[id].Emails, email)
	}
	return emails.Err()
}

// Remove soft-deletes userID's key from now on; false means no such key of theirs. Removing twice keeps the first date.
func (s *GPGKeyStore) Remove(ctx context.Context, keyID, userID int64) (bool, error) {
	return execOwned(ctx, s.db, "gpg key remove",
		`UPDATE gpg_keys SET removed_at = COALESCE(removed_at, NOW()) WHERE id = $1 AND user_id = $2`, keyID, userID)
}

// Revoke marks userID's key compromised; false means no such key of theirs.
func (s *GPGKeyStore) Revoke(ctx context.Context, keyID, userID int64) (bool, error) {
	return execOwned(ctx, s.db, "gpg key revoke",
		`UPDATE gpg_keys SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1 AND user_id = $2`, keyID, userID)
}

// Restore re-activates userID's removed key. found is false when userID has no such key; revoked when it is revoked
// and so stays as it is.
func (s *GPGKeyStore) Restore(ctx context.Context, keyID, userID int64) (found, revoked bool, err error) {
	ok, err := execOwned(ctx, s.db, "gpg key restore",
		`UPDATE gpg_keys SET removed_at = NULL WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, keyID, userID)
	if err != nil || ok {
		return ok, false, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT revoked_at IS NOT NULL FROM gpg_keys WHERE id = $1 AND user_id = $2`, keyID, userID).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return err == nil, revoked, err
}

// execOwned runs an UPDATE scoped to one owner's row; false means it matched nothing.
func execOwned(ctx context.Context, db *sql.DB, op, query string, args ...any) (bool, error) {
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
