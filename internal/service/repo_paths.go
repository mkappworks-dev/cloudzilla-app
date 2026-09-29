package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidRepoPath = errors.New("invalid repository path")

// RepoDir returns root/owner/name. It refuses any owner or name that isn't a
// single clean path element, so no stored or requested name can reach outside root.
// Glob metacharacters are refused too, so a returned path, used as a pattern,
// matches only itself.
func RepoDir(root, owner, name string) (string, error) {
	if !isPathElement(owner) || !isPathElement(name) {
		return "", fmt.Errorf("%w: %q/%q", ErrInvalidRepoPath, owner, name)
	}
	return filepath.Join(root, owner, name), nil
}

func isPathElement(seg string) bool {
	return seg != "" && seg != "." && seg != ".." &&
		filepath.Base(seg) == seg && !strings.ContainsAny(seg, "/\\\x00*?[]")
}

const deletedDirInfix = ".deleted."

func deletedDirPath(repoPath string, at time.Time) string {
	return repoPath + deletedDirInfix + strconv.FormatInt(at.Unix(), 10)
}

// deletedDirs lists the soft-deleted copies of repoPath in name order, so the
// last one is the latest.
func deletedDirs(repoPath string) ([]string, error) {
	parent := filepath.Dir(repoPath)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prefix := filepath.Base(repoPath) + deletedDirInfix
	var dirs []string
	for _, e := range entries {
		stamp, ok := strings.CutPrefix(e.Name(), prefix)
		if ok && stamp != "" && strings.Trim(stamp, "0123456789") == "" {
			dirs = append(dirs, filepath.Join(parent, e.Name()))
		}
	}
	return dirs, nil
}
