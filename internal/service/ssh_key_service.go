package service

import (
	"context"
	"crypto/md5"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
	"golang.org/x/crypto/ssh"
)

type SSHKeyService struct {
	keys  *store.SSHKeyStore
	users *store.UserStore
}

func NewSSHKeyService(keys *store.SSHKeyStore, users *store.UserStore) *SSHKeyService {
	return &SSHKeyService{keys: keys, users: users}
}

func (s *SSHKeyService) AddKey(ctx context.Context, userID int64, title, rawPublicKey string) (*model.SSHKey, error) {
	// Parse and validate the public key
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(rawPublicKey))
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// Compute MD5 fingerprint
	fingerprint := computeFingerprint(pubKey)

	key := &model.SSHKey{
		UserID:      userID,
		Title:       title,
		PublicKey:   rawPublicKey,
		Fingerprint: fingerprint,
	}

	if err := s.keys.Create(ctx, key); err != nil {
		return nil, err
	}

	return key, nil
}

func (s *SSHKeyService) ListByUser(ctx context.Context, userID int64) ([]model.SSHKey, error) {
	return s.keys.ListByUser(ctx, userID)
}

func (s *SSHKeyService) Delete(ctx context.Context, keyID, callerUserID int64) error {
	return s.keys.Delete(ctx, keyID, callerUserID)
}

func (s *SSHKeyService) AuthenticatePublicKey(ctx context.Context, pubKey ssh.PublicKey) (*model.User, error) {
	fingerprint := computeFingerprint(pubKey)
	key, err := s.keys.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("key not found: %w", err)
	}

	user, err := s.users.GetByID(ctx, key.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return user, nil
}

func computeFingerprint(pubKey ssh.PublicKey) string {
	hash := md5.Sum(pubKey.Marshal())
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x",
		hash[0], hash[1], hash[2], hash[3], hash[4], hash[5], hash[6], hash[7],
		hash[8], hash[9], hash[10], hash[11], hash[12], hash[13], hash[14], hash[15])
}
