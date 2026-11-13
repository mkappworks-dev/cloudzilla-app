package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	ciSecretPurpose = "ci-secret"
	// MaxCISecrets and MaxCISecretBytes keep a claim response and the masker small.
	MaxCISecrets     = 100
	MaxCISecretBytes = 16 << 10
)

var (
	ErrCISecretNoKey     = errors.New("secrets need security.secret_key, which is not set on this Instance")
	ErrCISecretName      = errors.New("a secret name is letters, digits and underscores, not starting with a digit, up to 100 characters")
	ErrCISecretValue     = errors.New("a secret value is 1 byte to 16 KiB")
	ErrCISecretLimit     = errors.New("a repository can hold 100 secrets")
	ErrCISecretForbidden = errors.New("forbidden: managing this repository is required")
	ErrCISecretNotFound  = errors.New("secret not found")
)

var ciSecretNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,99}$`)

// CISecretService keeps repo secrets sealed with security.secret_key. Values go in and are never read back by a page:
// only the claim response and the log masker open them.
type CISecretService struct {
	secrets *store.CISecretStore
	box     *secretbox.Box
	repoSvc *RepoService
}

func NewCISecretService(secrets *store.CISecretStore, box *secretbox.Box, repoSvc *RepoService) *CISecretService {
	return &CISecretService{secrets: secrets, box: box, repoSvc: repoSvc}
}

// HasKey reports whether secrets can be saved on this Instance.
func (s *CISecretService) HasKey() bool { return s.box != nil }

// List returns the repo's secrets without values.
func (s *CISecretService) List(ctx context.Context, repoID int64) ([]model.CISecret, error) {
	rows, err := s.secrets.List(ctx, repoID, false)
	if err != nil {
		return nil, err
	}
	out := make([]model.CISecret, len(rows))
	for i, r := range rows {
		out[i] = model.CISecret{ID: r.ID, RepoID: repoID, Name: r.Name, CreatedBy: r.CreatedBy, CreatedByName: r.CreatedByName,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}

// Save adds a secret or replaces the value of the one with this name.
func (s *CISecretService) Save(ctx context.Context, repo *model.Repository, userID int64, name, value string) error {
	if !s.repoSvc.CanManage(ctx, repo, userID) {
		return ErrCISecretForbidden
	}
	if !ciSecretNameRe.MatchString(name) {
		return ErrCISecretName
	}
	if value == "" || len(value) > MaxCISecretBytes {
		return ErrCISecretValue
	}
	sealed, err := s.box.Seal(ciSecretPurpose, []byte(value))
	if errors.Is(err, secretbox.ErrNoKey) {
		return ErrCISecretNoKey
	}
	if err != nil {
		return fmt.Errorf("seal ci secret: %w", err)
	}
	exists, err := s.secrets.Exists(ctx, repo.ID, name)
	if err != nil {
		return err
	}
	if !exists {
		if n, err := s.secrets.Count(ctx, repo.ID); err != nil {
			return err
		} else if n >= MaxCISecrets {
			return ErrCISecretLimit
		}
	}
	return s.secrets.Upsert(ctx, repo.ID, name, sealed, userID)
}

func (s *CISecretService) Delete(ctx context.Context, repo *model.Repository, userID int64, name string) error {
	if !s.repoSvc.CanManage(ctx, repo, userID) {
		return ErrCISecretForbidden
	}
	ok, err := s.secrets.Delete(ctx, repo.ID, name)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCISecretNotFound
	}
	return nil
}

// Values returns the repo's secrets by name, opened. A value that no longer opens (the key changed) is left out and
// logged, so one bad row cannot fail a claim.
func (s *CISecretService) Values(ctx context.Context, repoID int64) (map[string]string, error) {
	rows, err := s.secrets.List(ctx, repoID, true)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		v, err := s.box.Open(ciSecretPurpose, r.ValueEnc)
		if err != nil {
			slog.Warn("ci secret does not open; leaving it out", "repo_id", repoID, "name", r.Name, "error", err)
			continue
		}
		out[r.Name] = string(v)
	}
	return out, nil
}

// SecretValues implements SecretSource for the server-side log backstop.
func (s *CISecretService) SecretValues(ctx context.Context, repoID int64) ([]string, error) {
	m, err := s.Values(ctx, repoID)
	if err != nil {
		return nil, err
	}
	vals := make([]string, 0, len(m))
	for _, v := range m {
		vals = append(vals, v)
	}
	slices.Sort(vals)
	return vals, nil
}
