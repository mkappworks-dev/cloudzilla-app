package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
)

// LocalBackend keeps objects as files under a root directory. Every access
// goes through an os.Root, so no key can resolve outside it, symlinks included.
type LocalBackend struct {
	root *os.Root
}

func NewLocal(dir string) (*LocalBackend, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create local root: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: open local root: %w", err)
	}
	return &LocalBackend{root: root}, nil
}

func (b *LocalBackend) Close() error { return b.root.Close() }

// Put writes a temp file beside the target and renames it into place, so a
// reader sees either the old object or the whole new one.
func (b *LocalBackend) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	if err := checkKey(key); err != nil {
		return err
	}
	dir := path.Dir(key)
	if dir != "." {
		if err := b.root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("storage: create dir: %w", err)
		}
	}
	var suffix [8]byte
	_, _ = rand.Read(suffix[:])
	tmp := path.Join(dir, ".tmp-"+hex.EncodeToString(suffix[:]))
	f, err := b.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("storage: create temp: %w", err)
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = b.root.Rename(tmp, key)
	}
	if err != nil {
		_ = b.root.Remove(tmp)
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

func (b *LocalBackend) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	f, err := b.root.Open(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	if st, err := f.Stat(); err != nil || st.IsDir() {
		_ = f.Close()
		return nil, ErrNotFound
	}
	return f, nil
}

func (b *LocalBackend) Delete(_ context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := b.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

func (b *LocalBackend) Exists(_ context.Context, key string) (bool, error) {
	if err := checkKey(key); err != nil {
		return false, err
	}
	st, err := b.root.Stat(key)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: stat %s: %w", key, err)
	}
	return !st.IsDir(), nil
}
