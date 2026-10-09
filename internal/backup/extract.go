package backup

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// extractor materialises validated git-repos entries below root. Files are
// created with O_EXCL: a duplicate entry fails instead of overwriting.
type extractor struct{ root string }

func (x extractor) dir(rel string, mode fs.FileMode) error {
	target, err := x.target(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(target, mode.Perm()|0o700)
}

func (x extractor) file(rel string, r io.Reader, mode fs.FileMode, mod time.Time) (int64, error) {
	target, err := x.target(rel)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm()|0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	return n, os.Chtimes(target, mod, mod)
}

// target re-checks containment even though checkEntry already vetted rel.
func (x extractor) target(rel string) (string, error) {
	local := filepath.FromSlash(rel)
	if rel != "" && !filepath.IsLocal(local) {
		return "", fmt.Errorf("%q escapes the repos root", rel)
	}
	return filepath.Join(x.root, local), nil
}

// rootEmpty is nil when dir is missing or holds nothing.
func rootEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read repos root: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("git.repos_root %s is not empty: restore only runs into an empty directory", dir)
	}
	return nil
}
