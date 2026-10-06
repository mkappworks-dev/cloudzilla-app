// Package storagetest is the conformance suite every storage.Backend passes.
package storagetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
)

// Run exercises b. Keys are made unique with prefix, so the suite can share a
// real bucket with other runs.
func Run(t *testing.T, b storage.Backend, prefix string) {
	t.Helper()
	ctx := context.Background()
	key := func(k string) string { return prefix + "/" + k }

	t.Run("PutGetExistsDelete", func(t *testing.T) {
		k := key("a/b/object.png")
		put(t, b, k, "hello")
		if got := get(t, b, k); got != "hello" {
			t.Errorf("Get = %q, want hello", got)
		}
		if ok, err := b.Exists(ctx, k); err != nil || !ok {
			t.Errorf("Exists = %v, %v; want true", ok, err)
		}
		if err := b.Delete(ctx, k); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if ok, err := b.Exists(ctx, k); err != nil || ok {
			t.Errorf("Exists after delete = %v, %v; want false", ok, err)
		}
	})

	t.Run("Overwrite", func(t *testing.T) {
		k := key("overwrite.jpg")
		put(t, b, k, "first")
		put(t, b, k, "second")
		if got := get(t, b, k); got != "second" {
			t.Errorf("Get = %q, want second", got)
		}
		_ = b.Delete(ctx, k)
	})

	t.Run("GetMissingIsErrNotFound", func(t *testing.T) {
		rc, err := b.Get(ctx, key("missing.png"))
		if rc != nil {
			_ = rc.Close()
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Get missing: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("DeleteMissingIsNil", func(t *testing.T) {
		if err := b.Delete(ctx, key("never-written.png")); err != nil {
			t.Errorf("Delete missing: %v", err)
		}
	})

	t.Run("InvalidKeysRefused", func(t *testing.T) {
		for _, k := range []string{"", "/abs", "trailing/", "a//b", "../escape", "a/../../b", ".", "UPPER", "sp ace", `back\slash`} {
			if err := b.Put(ctx, k, bytes.NewReader([]byte("x")), 1); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("Put(%q): err = %v, want ErrInvalidKey", k, err)
			}
			if _, err := b.Get(ctx, k); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("Get(%q): err = %v, want ErrInvalidKey", k, err)
			}
			if _, err := b.Exists(ctx, k); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("Exists(%q): err = %v, want ErrInvalidKey", k, err)
			}
			if err := b.Delete(ctx, k); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("Delete(%q): err = %v, want ErrInvalidKey", k, err)
			}
		}
	})
}

func put(t *testing.T, b storage.Backend, key, body string) {
	t.Helper()
	if err := b.Put(context.Background(), key, bytes.NewReader([]byte(body)), int64(len(body))); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}

func get(t *testing.T, b storage.Backend, key string) string {
	t.Helper()
	rc, err := b.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	return string(data)
}
