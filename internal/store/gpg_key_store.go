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

func (s *GPGKeyStore) Create(ctx context.Context, key *model.GPGKey) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gpg key create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx,
		`INSERT INTO gpg_keys (user_id, key_id, fingerprint, public_key, expires_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		key.UserID, key.KeyID, key.Fingerprint, key.PublicKey, key.ExpiresAt, key.RevokedAt,
	).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return signingKeyInsertError("gpg key create", err)
	}
	for i := range key.Subkeys {
		sk := &key.Subkeys[i]
		sk.GPGKeyID = key.ID
		err = tx.QueryRowContext(ctx,
			`INSERT INTO gpg_subkeys (gpg_key_id, key_id, fingerprint, can_sign, expires_at, revoked_at)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			key.ID, sk.KeyID, sk.Fingerprint, sk.CanSign, sk.ExpiresAt, sk.RevokedAt,
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
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gpg key create: %w", err)
	}
	return nil
}

func signingKeyInsertError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrSigningKeyTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (s *GPGKeyStore) ListByUser(ctx context.Context, userID int64) ([]model.GPGKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, key_id, fingerprint, public_key, expires_at, revoked_at, created_at
		 FROM gpg_keys WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("gpg keys list: %w", err)
	}
	defer rows.Close()
	var keys []model.GPGKey
	byID := map[int64]*model.GPGKey{}
	for rows.Next() {
		var k model.GPGKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyID, &k.Fingerprint, &k.PublicKey, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		k.Subkeys, k.Emails = []model.GPGSubkey{}, []string{}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range keys {
		byID[keys[i].ID] = &keys[i]
	}
	if len(keys) == 0 {
		return keys, nil
	}

	subs, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.gpg_key_id, s.key_id, s.fingerprint, s.can_sign, s.expires_at, s.revoked_at
		 FROM gpg_subkeys s JOIN gpg_keys k ON k.id = s.gpg_key_id WHERE k.user_id = $1 ORDER BY s.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("gpg subkeys list: %w", err)
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
		`SELECT e.gpg_key_id, e.email FROM gpg_key_emails e JOIN gpg_keys k ON k.id = e.gpg_key_id
		 WHERE k.user_id = $1 ORDER BY e.email`, userID)
	if err != nil {
		return nil, fmt.Errorf("gpg key emails list: %w", err)
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

// Delete removes userID's key; false means no such key of theirs.
func (s *GPGKeyStore) Delete(ctx context.Context, keyID, userID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM gpg_keys WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		return false, fmt.Errorf("gpg key delete: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
