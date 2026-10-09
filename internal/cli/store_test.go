package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeKeyring struct {
	val string
	err error
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{} }

func (f *fakeKeyring) Set(v string) error {
	if f.err != nil {
		return f.err
	}
	f.val = v
	return nil
}

func (f *fakeKeyring) Get() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.val == "" {
		return "", ErrNoEntry
	}
	return f.val, nil
}

func (f *fakeKeyring) Delete() error {
	if f.err != nil {
		return f.err
	}
	f.val = ""
	return nil
}

var creds = Credentials{Host: "https://h", Token: "czp_tok"}

func TestStoreKeychainRoundTrip(t *testing.T) {
	kr := newFakeKeyring()
	path := filepath.Join(t.TempDir(), "cz", "c.json")
	st := &Store{Keyring: kr, Path: path}
	where, err := st.Save(creds)
	if err != nil || where != "keychain" {
		t.Fatalf("where=%q err=%v", where, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file written although keychain worked")
	}
	got, where, err := st.Load()
	if err != nil || got != creds || where != "keychain" {
		t.Fatalf("got=%+v where=%q err=%v", got, where, err)
	}
	if err := st.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Load(); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v", err)
	}
}

func TestStoreFallsBackToFile(t *testing.T) {
	kr := &fakeKeyring{err: errors.New("no dbus")}
	path := filepath.Join(t.TempDir(), "cz", "c.json")
	st := &Store{Keyring: kr, Path: path}
	where, err := st.Save(creds)
	if err != nil || !strings.HasPrefix(where, "file") || !strings.Contains(where, path) {
		t.Fatalf("where=%q err=%v", where, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	got, where, err := st.Load()
	if err != nil || got != creds || !strings.HasPrefix(where, "file") {
		t.Fatalf("got=%+v where=%q err=%v", got, where, err)
	}
	if err := st.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file survives logout")
	}
}

func TestStoreInsecureForcesFile(t *testing.T) {
	kr := newFakeKeyring()
	st := &Store{Keyring: kr, Path: filepath.Join(t.TempDir(), "c.json"), Insecure: true}
	where, err := st.Save(creds)
	if err != nil || !strings.HasPrefix(where, "file") {
		t.Fatalf("where=%q err=%v", where, err)
	}
	if kr.val != "" {
		t.Error("keychain used despite Insecure")
	}
}

func TestStoreSaveMovesBetweenBackends(t *testing.T) {
	kr := newFakeKeyring()
	path := filepath.Join(t.TempDir(), "c.json")
	st := &Store{Keyring: kr, Path: path, Insecure: true}
	_, _ = st.Save(creds)
	st.Insecure = false
	if _, err := st.Save(creds); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale plaintext file left after keychain save")
	}
}

func TestStoreNilKeyring(t *testing.T) {
	st := &Store{Path: filepath.Join(t.TempDir(), "c.json")}
	if _, err := st.Save(creds); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Load(); err != nil {
		t.Fatal(err)
	}
}
