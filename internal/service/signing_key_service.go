package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"golang.org/x/crypto/ssh"
)

var (
	// ErrSigningKeyTaken means another account holds the key; ErrSigningKeyOwned that the adding account already does.
	ErrSigningKeyTaken    = store.ErrSigningKeyTaken
	ErrSigningKeyOwned    = errors.New("signing key already on your account")
	ErrSigningKeyNotFound = errors.New("signing key not found")
	// ErrSigningKeyRevoked is final: a revoked key cannot be restored, updated or added again.
	ErrSigningKeyRevoked = errors.New("this key was revoked and cannot be used again")
)

// SigningKeyService manages the GPG and SSH public keys that sign commits. They are separate from the
// authentication keys of SSHKeyService: the same SSH key may be registered as both.
//
// Remove soft-deletes a key: it keeps verifying commits dated before the removal. Revoke marks it compromised: none of
// its commits verify. Both are kept as rows so the fingerprint stays reserved to the account.
type SigningKeyService struct {
	gpg *store.GPGKeyStore
	ssh *store.SSHSigningKeyStore
}

func NewSigningKeyService(gpg *store.GPGKeyStore, sshKeys *store.SSHSigningKeyStore) *SigningKeyService {
	return &SigningKeyService{gpg: gpg, ssh: sshKeys}
}

// AddGPGKey is UpsertGPGKey for callers that do not care whether the key was new.
func (s *SigningKeyService) AddGPGKey(ctx context.Context, userID int64, armored string) (*model.GPGKey, error) {
	key, _, err := s.UpsertGPGKey(ctx, userID, armored)
	return key, err
}

// UpsertGPGKey registers a new key, or refreshes one the account already owns from an updated block: new expiry,
// subkeys, UID emails or a revocation certificate. created reports which. A revoked key stays revoked.
func (s *SigningKeyService) UpsertGPGKey(ctx context.Context, userID int64, armored string) (key *model.GPGKey, created bool, err error) {
	now := time.Now()
	p, err := readGPGKey(armored, now)
	if err != nil {
		return nil, false, err
	}
	existing, err := s.gpg.FindByFingerprint(ctx, p.key.Fingerprint)
	if err != nil {
		return nil, false, err
	}
	switch {
	case existing == nil:
		if _, err := parseGPGKey(armored, now); err != nil {
			return nil, false, err
		}
		p.key.UserID = userID
		if err := s.gpg.Create(ctx, p.key); err != nil {
			if errors.Is(err, ErrSigningKeyTaken) {
				return nil, false, s.classifyGPGTaken(ctx, userID, p.key.Fingerprint, err)
			}
			return nil, false, err
		}
		return p.key, true, nil
	case existing.UserID != userID:
		return nil, false, ErrSigningKeyTaken
	case existing.RevokedAt != nil:
		return nil, false, ErrSigningKeyRevoked
	}

	p.key.ID, p.key.UserID = existing.ID, userID
	// The block restores a removed key unless it carries its own revocation; a stale copy cannot take a subkey's
	// revocation back.
	for i := range p.key.Subkeys {
		sk := &p.key.Subkeys[i]
		for _, old := range existing.Subkeys {
			if old.KeyID != sk.KeyID {
				continue
			}
			if old.RevokedAt != nil {
				sk.RevokedAt = old.RevokedAt
			}
			if sk.RemovedAt == nil {
				sk.RemovedAt = old.RemovedAt
			}
		}
	}
	if err := s.gpg.Update(ctx, p.key); err != nil {
		return nil, false, err
	}
	return p.key, false, nil
}

// ListGPGKeys lists userID's keys; removed ones only with includeRemoved.
func (s *SigningKeyService) ListGPGKeys(ctx context.Context, userID int64, includeRemoved bool) ([]model.GPGKey, error) {
	return s.gpg.ListByUser(ctx, userID, includeRemoved)
}

func (s *SigningKeyService) RemoveGPGKey(ctx context.Context, keyID, userID int64) error {
	return foundOrNot(s.gpg.Remove(ctx, keyID, userID))
}

func (s *SigningKeyService) RevokeGPGKey(ctx context.Context, keyID, userID int64) error {
	return foundOrNot(s.gpg.Revoke(ctx, keyID, userID))
}

func (s *SigningKeyService) RestoreGPGKey(ctx context.Context, keyID, userID int64) error {
	return restored(s.gpg.Restore(ctx, keyID, userID))
}

// AddSSHKey is UpsertSSHKey for callers that do not care whether the key was new.
func (s *SigningKeyService) AddSSHKey(ctx context.Context, userID int64, title, rawPublicKey string) (*model.SSHSigningKey, error) {
	key, _, err := s.UpsertSSHKey(ctx, userID, title, rawPublicKey)
	return key, err
}

// UpsertSSHKey registers a new key. Posting a key the account already owns returns it, restoring it first when it was
// removed. created reports whether a row was inserted.
func (s *SigningKeyService) UpsertSSHKey(ctx context.Context, userID int64, title, rawPublicKey string) (key *model.SSHSigningKey, created bool, err error) {
	normalized, err := parseSigningKey(rawPublicKey)
	if err != nil {
		return nil, false, err
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(normalized)))
	if err != nil {
		return nil, false, ErrInvalidSigningKey
	}
	fingerprint := ssh.FingerprintSHA256(pub)
	existing, err := s.ssh.FindByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, false, err
	}
	switch {
	case existing == nil:
		key = &model.SSHSigningKey{UserID: userID, Title: title, PublicKey: normalized, Fingerprint: fingerprint}
		if err := s.ssh.Create(ctx, key); err != nil {
			if errors.Is(err, ErrSigningKeyTaken) {
				return nil, false, s.classifySSHTaken(ctx, userID, key.Fingerprint, err)
			}
			return nil, false, err
		}
		return key, true, nil
	case existing.UserID != userID:
		return nil, false, ErrSigningKeyTaken
	case existing.RevokedAt != nil:
		return nil, false, ErrSigningKeyRevoked
	case existing.RemovedAt != nil:
		if err := s.RestoreSSHKey(ctx, existing.ID, userID); err != nil {
			return nil, false, err
		}
		existing.RemovedAt, existing.Status = nil, model.SigningKeyActive
	}
	return existing, false, nil
}

// ListSSHKeys lists userID's keys; removed ones only with includeRemoved.
func (s *SigningKeyService) ListSSHKeys(ctx context.Context, userID int64, includeRemoved bool) ([]model.SSHSigningKey, error) {
	return s.ssh.ListByUser(ctx, userID, includeRemoved)
}

func (s *SigningKeyService) RemoveSSHKey(ctx context.Context, keyID, userID int64) error {
	return foundOrNot(s.ssh.Remove(ctx, keyID, userID))
}

func (s *SigningKeyService) RevokeSSHKey(ctx context.Context, keyID, userID int64) error {
	return foundOrNot(s.ssh.Revoke(ctx, keyID, userID))
}

func (s *SigningKeyService) RestoreSSHKey(ctx context.Context, keyID, userID int64) error {
	return restored(s.ssh.Restore(ctx, keyID, userID))
}

func foundOrNot(ok bool, err error) error {
	if err == nil && !ok {
		return ErrSigningKeyNotFound
	}
	return err
}

// classifyGPGTaken tells a key the user already holds from one another account holds, so the 409 can say which. A
// failed lookup keeps the plain taken error.
func (s *SigningKeyService) classifyGPGTaken(ctx context.Context, userID int64, fingerprint string, taken error) error {
	keys, err := s.gpg.ListByUser(ctx, userID, true)
	if err != nil {
		return taken
	}
	for _, k := range keys {
		if k.Fingerprint == fingerprint {
			return ErrSigningKeyOwned
		}
	}
	return taken
}

func (s *SigningKeyService) classifySSHTaken(ctx context.Context, userID int64, fingerprint string, taken error) error {
	keys, err := s.ssh.ListByUser(ctx, userID, true)
	if err != nil {
		return taken
	}
	for _, k := range keys {
		if k.Fingerprint == fingerprint {
			return ErrSigningKeyOwned
		}
	}
	return taken
}

func restored(found, revoked bool, err error) error {
	switch {
	case err != nil:
		return err
	case !found:
		return ErrSigningKeyNotFound
	case revoked:
		return ErrSigningKeyRevoked
	}
	return nil
}
