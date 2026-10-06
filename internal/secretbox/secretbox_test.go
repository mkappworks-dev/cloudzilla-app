package secretbox_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
)

var (
	keyA = []byte(strings.Repeat("a", 32))
	keyB = []byte(strings.Repeat("b", 32))
)

func mustNew(t *testing.T, key []byte) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return box
}

func TestBox_RoundTrip(t *testing.T) {
	box := mustNew(t, keyA)
	sealed, err := box.Seal("mirror-credential", []byte("ghp_token"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed, []byte("ghp_token")) {
		t.Fatal("sealed value contains the plaintext")
	}
	got, err := box.Open("mirror-credential", sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != "ghp_token" {
		t.Errorf("Open = %q, want ghp_token", got)
	}
}

func TestBox_SealIsRandomized(t *testing.T) {
	box := mustNew(t, keyA)
	a, _ := box.Seal("p", []byte("same"))
	b, _ := box.Seal("p", []byte("same"))
	if bytes.Equal(a, b) {
		t.Error("two seals of the same plaintext are identical")
	}
}

func TestBox_Open_Refuses(t *testing.T) {
	sealed, err := mustNew(t, keyA).Seal("mirror-credential", []byte("ghp_token"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 1
	version := bytes.Clone(sealed)
	version[0] = 0x7f

	tests := []struct {
		name       string
		key        []byte
		purpose    string
		ciphertext []byte
	}{
		{"other key", keyB, "mirror-credential", sealed},
		{"other purpose", keyA, "webhook-secret", sealed},
		{"tampered", keyA, "mirror-credential", flipped},
		{"unknown version", keyA, "mirror-credential", version},
		{"truncated", keyA, "mirror-credential", sealed[:10]},
		{"empty", keyA, "mirror-credential", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mustNew(t, tt.key).Open(tt.purpose, tt.ciphertext)
			if !errors.Is(err, secretbox.ErrUndecryptable) {
				t.Errorf("Open err = %v, want ErrUndecryptable", err)
			}
		})
	}
}

func TestNew_ShortKeyRefused(t *testing.T) {
	_, err := secretbox.New([]byte(strings.Repeat("k", 31)))
	if err == nil {
		t.Fatal("New accepted a 31-byte key")
	}
	if strings.Contains(err.Error(), "kkkk") {
		t.Errorf("error %q leaks the key", err)
	}
}

func TestNew_EmptyKeyGivesNilBox(t *testing.T) {
	box, err := secretbox.New(nil)
	if err != nil || box != nil {
		t.Errorf("New(nil) = %v, %v; want nil, nil", box, err)
	}
}

func TestNilBox_ReportsNoKey(t *testing.T) {
	var box *secretbox.Box
	if _, err := box.Seal("p", []byte("x")); !errors.Is(err, secretbox.ErrNoKey) {
		t.Errorf("Seal err = %v, want ErrNoKey", err)
	}
	if _, err := box.Open("p", []byte("x")); !errors.Is(err, secretbox.ErrNoKey) {
		t.Errorf("Open err = %v, want ErrNoKey", err)
	}
}
