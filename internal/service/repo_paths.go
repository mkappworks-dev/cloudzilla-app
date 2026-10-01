package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
