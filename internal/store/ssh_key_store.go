package store

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type SSHKeyStore struct{ q *db.Queries }

func NewSSHKeyStore(q *db.Queries) *SSHKeyStore { return &SSHKeyStore{q: q} }

func (s *SSHKeyStore) Create(ctx context.Context, key *model.SSHKey) error {
	result, err := s.q.CreateSSHKey(ctx, db.CreateSSHKeyParams{
		UserID:      key.UserID,
		Title:       key.Title,
		PublicKey:   key.PublicKey,
		Fingerprint: key.Fingerprint,
	})
	if err != nil {
		return fmt.Errorf("ssh key create: %w", err)
	}
	key.ID = result.ID
	key.CreatedAt = result.CreatedAt
	return nil
}

func (s *SSHKeyStore) ListByUser(ctx context.Context, userID int64) ([]model.SSHKey, error) {
	results, err := s.q.ListSSHKeysByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("ssh keys list by user: %w", err)
	}
	keys := make([]model.SSHKey, len(results))
	for i, r := range results {
		keys[i] = mapDBSSHKeyToModel(&r)
	}
	return keys, nil
}

func (s *SSHKeyStore) GetByFingerprint(ctx context.Context, fingerprint string) (*model.SSHKey, error) {
	result, err := s.q.GetSSHKeyByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("ssh key get by fingerprint: %w", err)
	}
	key := mapDBSSHKeyToModel(&result)
	return &key, nil
}

func (s *SSHKeyStore) Delete(ctx context.Context, keyID, userID int64) error {
	err := s.q.DeleteSSHKey(ctx, db.DeleteSSHKeyParams{
		ID:     keyID,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("ssh key delete: %w", err)
	}
	return nil
}

func mapDBSSHKeyToModel(dbKey *db.SshKey) model.SSHKey {
	return model.SSHKey{
		ID:          dbKey.ID,
		UserID:      dbKey.UserID,
		Title:       dbKey.Title,
		PublicKey:   dbKey.PublicKey,
		Fingerprint: dbKey.Fingerprint,
		CreatedAt:   dbKey.CreatedAt,
	}
}
