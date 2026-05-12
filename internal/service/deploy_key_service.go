package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	gossh "golang.org/x/crypto/ssh"
)

// DeployKeyService manages repository deploy keys used for SSH authentication.
type DeployKeyService struct {
	keys    *store.DeployKeyStore
	sshKeys *store.SSHKeyStore
}

// NewDeployKeyService creates a DeployKeyService backed by the given stores.
func NewDeployKeyService(keys *store.DeployKeyStore, sshKeys *store.SSHKeyStore) *DeployKeyService {
	return &DeployKeyService{keys: keys, sshKeys: sshKeys}
}

// Add parses and validates the public key, checks uniqueness, then stores the deploy key.
func (s *DeployKeyService) Add(ctx context.Context, repoID int64, title, rawPublicKey string, readOnly bool) (*model.DeployKey, error) {
	pubKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(rawPublicKey))
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// computeFingerprint is package-level in ssh_key_service.go (same package)
	fingerprint := computeFingerprint(pubKey)

	// Cross-table uniqueness: reject if already registered as a user SSH key
	if _, err := s.sshKeys.GetByFingerprint(ctx, fingerprint); err == nil {
		return nil, errors.New("this key is already registered as a user SSH key")
	}

	k := &model.DeployKey{
		RepoID:      repoID,
		Title:       title,
		Fingerprint: fingerprint,
		PublicKey:   rawPublicKey,
		ReadOnly:    readOnly,
	}
	if err := s.keys.Create(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

func (s *DeployKeyService) List(ctx context.Context, repoID int64) ([]model.DeployKey, error) {
	return s.keys.ListByRepo(ctx, repoID)
}

func (s *DeployKeyService) Delete(ctx context.Context, keyID, repoID int64) error {
	return s.keys.Delete(ctx, keyID, repoID)
}

// AuthenticatePublicKey looks up a deploy key by fingerprint during SSH handshake.
func (s *DeployKeyService) AuthenticatePublicKey(ctx context.Context, pubKey gossh.PublicKey) (*model.DeployKey, error) {
	fingerprint := computeFingerprint(pubKey)
	dk, err := s.keys.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("deploy key not found")
		}
		return nil, err
	}
	return dk, nil
}

func (s *DeployKeyService) UpdateLastUsed(ctx context.Context, keyID int64) error {
	return s.keys.UpdateLastUsed(ctx, keyID)
}
