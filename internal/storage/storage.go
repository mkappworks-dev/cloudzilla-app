// Package storage keeps objects that live outside git and Postgres, on a local
// directory or an S3-compatible bucket.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

// Backend stores objects by key. Get on a missing key returns ErrNotFound;
// Delete on a missing key returns nil.
type Backend interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

var (
	ErrNotFound   = errors.New("storage: object not found")
	ErrInvalidKey = errors.New("storage: invalid key")
)

// ValidKey reports whether key is one or more "/"-joined segments of
// [a-z0-9._-], none of them empty, "." or "..".
func ValidKey(key string) bool {
	if key == "" {
		return false
	}
	for seg := range strings.SplitSeq(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
		for _, c := range seg {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
				return false
			}
		}
	}
	return true
}

func checkKey(key string) error {
	if !ValidKey(key) {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return nil
}

// New builds the configured backend. It checks the configuration only: an
// unreachable bucket fails the first request, not startup.
func New(ctx context.Context, cfg config.StorageConfig) (Backend, error) {
	switch cfg.Backend {
	case "", "local":
		if cfg.Local.Root == "" {
			return nil, errors.New("storage: storage.local.root is required for the local backend")
		}
		return NewLocal(cfg.Local.Root)
	case "s3":
		return NewS3(ctx, cfg.S3)
	default:
		return nil, fmt.Errorf("storage: unknown storage.backend %q (use local or s3)", cfg.Backend)
	}
}
