package storage_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage/storagetest"
)

func TestLocalBackend_Conformance(t *testing.T) {
	b, err := storage.NewLocal(filepath.Join(t.TempDir(), "nested", "root"))
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	storagetest.Run(t, b, "suite")
}

// A symlink planted inside the root must not let a key reach a file outside it.
func TestLocalBackend_SymlinkCannotEscapeRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	if _, err := b.Get(context.Background(), "link/secret"); err == nil {
		t.Error("Get through a symlink out of the root succeeded")
	}
	if err := b.Put(context.Background(), "link/planted", bytes.NewReader([]byte("x")), 1); err == nil {
		t.Error("Put through a symlink out of the root succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted")); !os.IsNotExist(err) {
		t.Errorf("a file was written outside the root: %v", err)
	}
}

func TestNew_RejectsBadConfig(t *testing.T) {
	cases := map[string]config.StorageConfig{
		"unknown backend":   {Backend: "gcs"},
		"s3 without bucket": {Backend: "s3"},
		"s3 half keys":      {Backend: "s3", S3: config.S3StorageConfig{Bucket: "b", AccessKeyID: "k"}},
		"s3 bad prefix":     {Backend: "s3", S3: config.S3StorageConfig{Bucket: "b", Prefix: "../x"}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := storage.New(context.Background(), cfg); err == nil {
				t.Error("want an error")
			}
		})
	}
}
