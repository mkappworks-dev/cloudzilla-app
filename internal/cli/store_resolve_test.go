package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSystemKeyring(t *testing.T) {
	keyring.MockInit()
	kr := systemKeyring{}

	if _, err := kr.Get(); !errors.Is(err, ErrNoEntry) {
		t.Errorf("Get on an empty keychain = %v, want ErrNoEntry", err)
	}
	if err := kr.Delete(); !errors.Is(err, ErrNoEntry) {
		t.Errorf("Delete on an empty keychain = %v, want ErrNoEntry", err)
	}
	if err := kr.Set("secret"); err != nil {
		t.Fatal(err)
	}
	if v, err := kr.Get(); err != nil || v != "secret" {
		t.Errorf("Get = %q, %v", v, err)
	}
	if err := kr.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := kr.Get(); !errors.Is(err, ErrNoEntry) {
		t.Errorf("Get after Delete = %v", err)
	}

	boom := errors.New("keychain locked")
	keyring.MockInitWithError(boom)
	t.Cleanup(keyring.MockInit)
	if err := kr.Set("x"); !errors.Is(err, boom) {
		t.Errorf("Set = %v", err)
	}
	if _, err := kr.Get(); !errors.Is(err, boom) || errors.Is(err, ErrNoEntry) {
		t.Errorf("Get = %v, want the keychain's own error", err)
	}
	if err := kr.Delete(); !errors.Is(err, boom) {
		t.Errorf("Delete = %v", err)
	}
}

func TestNewStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	s, err := NewStore(true)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Insecure || s.Keyring == nil {
		t.Errorf("store = %+v", s)
	}
	if !strings.HasPrefix(s.Path, home) || !strings.HasSuffix(s.Path, filepath.Join("cz", "credentials.json")) {
		t.Errorf("path = %q", s.Path)
	}
	if where, err := s.Save(creds); err != nil || where != "file "+s.Path {
		t.Fatalf("Save = %q, %v", where, err)
	}
	if got, _, err := s.Load(); err != nil || got != creds {
		t.Errorf("Load = %+v, %v", got, err)
	}
}

func TestStoreFileHasPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cz", "c.json")
	s := &Store{Path: path}
	if _, err := s.Save(creds); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("credentials file = %v, %v; want 0600", info, err)
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Errorf("credentials dir mode = %v, want 0700", info.Mode().Perm())
	}
	if left, _ := os.ReadDir(filepath.Dir(path)); len(left) != 1 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestStoreSaveFileErrors(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	kr := newFakeKeyring()
	kr.val = "old login"
	s := &Store{Keyring: kr, Path: filepath.Join(blocker, "cz", "c.json"), Insecure: true}
	if _, err := s.Save(creds); err == nil {
		t.Fatal("Save succeeded though the directory cannot be created")
	}
	if kr.val != "old login" {
		t.Error("the keychain entry was deleted though the file write failed")
	}
}

func TestStoreLoadErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		s := &Store{Path: filepath.Join(t.TempDir(), "c.json")}
		if _, _, err := s.Load(); !errors.Is(err, ErrNotLoggedIn) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		s := &Store{Path: t.TempDir()}
		if _, _, err := s.Load(); err == nil || errors.Is(err, ErrNotLoggedIn) || !strings.Contains(err.Error(), "reading") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("corrupt file", func(t *testing.T) {
		for _, body := range []string{"not json", `{"host":"https://h"}`} {
			path := filepath.Join(t.TempDir(), "c.json")
			_ = os.WriteFile(path, []byte(body), 0o600)
			s := &Store{Path: path}
			if _, _, err := s.Load(); err == nil || !strings.Contains(err.Error(), "corrupt") {
				t.Errorf("body %q: err = %v", body, err)
			}
		}
	})
	t.Run("unusable keychain entry falls through to the file", func(t *testing.T) {
		for _, entry := range []string{"not json", `{"host":"https://h"}`} {
			kr := newFakeKeyring()
			kr.val = entry
			path := filepath.Join(t.TempDir(), "c.json")
			s := &Store{Keyring: kr, Path: path}
			if _, err := (&Store{Path: path}).Save(creds); err != nil {
				t.Fatal(err)
			}
			got, where, err := s.Load()
			if err != nil || got != creds || where != "file "+path {
				t.Errorf("entry %q: got %+v, %q, %v", entry, got, where, err)
			}
		}
	})
}

func TestStoreDelete(t *testing.T) {
	kr := newFakeKeyring()
	path := filepath.Join(t.TempDir(), "c.json")
	s := &Store{Keyring: kr, Path: path, Insecure: true}
	if _, err := s.Save(creds); err != nil {
		t.Fatal(err)
	}
	kr.val = "stale"
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) || kr.val != "" {
		t.Errorf("file err = %v, keychain = %q; want both gone", err, kr.val)
	}
	if err := s.Delete(); err != nil {
		t.Errorf("Delete when already logged out = %v", err)
	}

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "child"), nil, 0o600)
	if err := (&Store{Path: dir}).Delete(); err == nil {
		t.Error("Delete of a non-empty directory reported success")
	}
}

func TestResolve(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	stored := func(t *testing.T) *Store {
		s := &Store{Keyring: newFakeKeyring(), Path: filepath.Join(t.TempDir(), "c.json")}
		if _, err := s.Save(creds); err != nil {
			t.Fatal(err)
		}
		return s
	}
	empty := func(t *testing.T) *Store {
		return &Store{Keyring: newFakeKeyring(), Path: filepath.Join(t.TempDir(), "c.json")}
	}

	cases := []struct {
		name      string
		env       func(string) string
		store     func(*testing.T) *Store
		want      Credentials
		wantWhere string
		wantErr   string
	}{
		{"both env vars win without touching the store", env("CZ_HOST", "https://env", "CZ_TOKEN", "t"), empty,
			Credentials{Host: "https://env", Token: "t"}, SourceEnv, ""},
		{"stored login", env(), stored, creds, "keychain", ""},
		{"token override keeps the stored host", env("CZ_TOKEN", "other"), stored,
			Credentials{Host: creds.Host, Token: "other"}, SourceEnv, ""},
		{"host override of the same host", env("CZ_HOST", creds.Host), stored, creds, "keychain", ""},
		{"host override of another host needs a token", env("CZ_HOST", "https://elsewhere"), stored, Credentials{}, "", "stored login is for"},
		{"token alone with no stored login", env("CZ_TOKEN", "t"), empty, Credentials{}, "", "no host"},
		{"nothing set", env(), empty, Credentials{}, "", "not logged in"},
		{"host alone with no stored login", env("CZ_HOST", "https://h"), empty, Credentials{}, "", "not logged in"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, where, err := Resolve(c.env, c.store(t))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want || where != c.wantWhere {
				t.Errorf("got %+v, %q, %v; want %+v, %q", got, where, err, c.want, c.wantWhere)
			}
		})
	}

	t.Run("a corrupt store is reported even when only the token is set", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "c.json")
		_ = os.WriteFile(path, []byte("junk"), 0o600)
		_, _, err := Resolve(env("CZ_TOKEN", "t"), &Store{Path: path})
		if err == nil || !strings.Contains(err.Error(), "corrupt") {
			t.Errorf("err = %v", err)
		}
	})
}
