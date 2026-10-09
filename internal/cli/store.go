package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

var (
	ErrNotLoggedIn = errors.New("not logged in; run `cz auth login` or set CZ_HOST and CZ_TOKEN")
	ErrNoEntry     = errors.New("no keychain entry")
)

const (
	SourceEnv = "environment (CZ_TOKEN)"

	keyringService = "cz"
	keyringUser    = "default"
	whereKeychain  = "keychain"
)

type Credentials struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

type Keyring interface {
	Set(value string) error
	Get() (string, error)
	Delete() error
}

type systemKeyring struct{}

func (systemKeyring) Set(v string) error { return keyring.Set(keyringService, keyringUser, v) }

func (systemKeyring) Get() (string, error) {
	v, err := keyring.Get(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoEntry
	}
	return v, err
}

func (systemKeyring) Delete() error {
	err := keyring.Delete(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNoEntry
	}
	return err
}

// Store keeps one login in the OS keychain, falling back to a 0600 file when the keychain is unavailable or Insecure is set.
type Store struct {
	Keyring  Keyring
	Path     string
	Insecure bool
}

func NewStore(insecure bool) (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("no user config dir: %w", err)
	}
	return &Store{Keyring: systemKeyring{}, Path: filepath.Join(dir, "cz", "credentials.json"), Insecure: insecure}, nil
}

// Save returns where the login went: "keychain" or "file <path>".
func (s *Store) Save(c Credentials) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	if !s.Insecure && s.Keyring != nil {
		if s.Keyring.Set(string(data)) == nil {
			_ = os.Remove(s.Path)
			return whereKeychain, nil
		}
	}
	if err := s.writeFile(data); err != nil {
		return "", err
	}
	if s.Keyring != nil {
		_ = s.Keyring.Delete()
	}
	return s.fileWhere(), nil
}

func (s *Store) Load() (Credentials, string, error) {
	if s.Keyring != nil {
		if v, err := s.Keyring.Get(); err == nil {
			var c Credentials
			if json.Unmarshal([]byte(v), &c) == nil && c.Token != "" {
				return c, whereKeychain, nil
			}
		}
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, "", ErrNotLoggedIn
		}
		return Credentials{}, "", fmt.Errorf("reading %s: %w", s.Path, err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil || c.Token == "" {
		return Credentials{}, "", fmt.Errorf("%s is corrupt; run `cz auth logout` and log in again", s.Path)
	}
	return c, s.fileWhere(), nil
}

func (s *Store) Delete() error {
	if s.Keyring != nil {
		_ = s.Keyring.Delete()
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) fileWhere() string { return "file " + s.Path }

func (s *Store) writeFile(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".credentials-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// Resolve takes CZ_HOST and CZ_TOKEN over the stored login, each independently.
func Resolve(getenv func(string) string, s *Store) (Credentials, string, error) {
	host, token := NormalizeHost(getenv("CZ_HOST")), getenv("CZ_TOKEN")
	if host != "" && token != "" {
		return Credentials{Host: host, Token: token}, SourceEnv, nil
	}
	stored, where, err := s.Load()
	if err != nil {
		if token != "" && errors.Is(err, ErrNotLoggedIn) {
			return Credentials{}, "", errors.New("CZ_TOKEN is set but no host; set CZ_HOST or run `cz auth login --host URL`")
		}
		return Credentials{}, "", err
	}
	if host != "" && host != stored.Host && token == "" {
		return Credentials{}, "", fmt.Errorf("CZ_HOST is %s but the stored login is for %s; set CZ_TOKEN too", host, stored.Host)
	}
	if host != "" {
		stored.Host = host
	}
	if token != "" {
		stored.Token = token
		where = SourceEnv
	}
	return stored, where, nil
}
