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
	ErrSigningKeyTaken    = store.ErrSigningKeyTaken
	ErrSigningKeyNotFound = errors.New("signing key not found")
)

// SigningKeyService manages the GPG and SSH public keys that sign commits. They are separate from the
// authentication keys of SSHKeyService: the same SSH key may be registered as both.
type SigningKeyService struct {
	gpg *store.GPGKeyStore
	ssh *store.SSHSigningKeyStore
}

func NewSigningKeyService(gpg *store.GPGKeyStore, sshKeys *store.SSHSigningKeyStore) *SigningKeyService {
	return &SigningKeyService{gpg: gpg, ssh: sshKeys}
}

func (s *SigningKeyService) AddGPGKey(ctx context.Context, userID int64, armored string) (*model.GPGKey, error) {
	key, err := parseGPGKey(armored, time.Now())
	if err != nil {
		return nil, err
	}
	key.UserID = userID
	if err := s.gpg.Create(ctx, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *SigningKeyService) ListGPGKeys(ctx context.Context, userID int64) ([]model.GPGKey, error) {
	return s.gpg.ListByUser(ctx, userID)
}

func (s *SigningKeyService) DeleteGPGKey(ctx context.Context, keyID, userID int64) error {
	ok, err := s.gpg.Delete(ctx, keyID, userID)
	if err == nil && !ok {
		return ErrSigningKeyNotFound
	}
	return err
}

func (s *SigningKeyService) AddSSHKey(ctx context.Context, userID int64, title, rawPublicKey string) (*model.SSHSigningKey, error) {
	normalized, err := parseSigningKey(rawPublicKey)
	if err != nil {
		return nil, err
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(normalized)))
	if err != nil {
		return nil, ErrInvalidSigningKey
	}
	key := &model.SSHSigningKey{
		UserID:      userID,
		Title:       title,
		PublicKey:   normalized,
		Fingerprint: ssh.FingerprintSHA256(pub),
	}
	if err := s.ssh.Create(ctx, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *SigningKeyService) ListSSHKeys(ctx context.Context, userID int64) ([]model.SSHSigningKey, error) {
	return s.ssh.ListByUser(ctx, userID)
}

func (s *SigningKeyService) DeleteSSHKey(ctx context.Context, keyID, userID int64) error {
	ok, err := s.ssh.Delete(ctx, keyID, userID)
	if err == nil && !ok {
		return ErrSigningKeyNotFound
	}
	return err
}
