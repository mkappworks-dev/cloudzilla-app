package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// deletedDirPath names dir's soft-deleted copy after its repository row, so
// restoring or purging one row can't take another row's copy.
func deletedDirPath(dir string, repoID int64) string {
	return dir + deletedDirInfix + "id" + strconv.FormatInt(repoID, 10)
}

// legacyStampSkew allows for the app's clock running ahead of the database's.
const legacyStampSkew = time.Minute

// legacyDeletedDir returns repoPath's copy soft-deleted before copies were named
// after their row, or "" if none. Delete stamped it with the Unix time just
// before the database set deletedAt, so the row's copy has the closest stamp,
// and one stamped well after deletedAt belongs to a later deletion.
func legacyDeletedDir(repoPath string, deletedAt time.Time) (string, error) {
	parent := filepath.Dir(repoPath)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	prefix := filepath.Base(repoPath) + deletedDirInfix
	target, latest := deletedAt.Unix(), deletedAt.Add(legacyStampSkew).Unix()
	var best string
	var bestGap int64
	for _, e := range entries {
		digits, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok || digits == "" || strings.Trim(digits, "0123456789") != "" {
			continue
		}
		stamp, err := strconv.ParseInt(digits, 10, 64)
		if err != nil || stamp > latest {
			continue
		}
		gap := max(target-stamp, stamp-target)
		if best == "" || gap < bestGap {
			best, bestGap = filepath.Join(parent, e.Name()), gap
		}
	}
	return best, nil
}

// deletedRepoDir returns the repository row's soft-deleted copy of repoPath,
// falling back to one deleted before copies were named after their row.
func deletedRepoDir(repoPath string, repoID int64, deletedAt *time.Time) (string, error) {
	bound := deletedDirPath(repoPath, repoID)
	if _, err := os.Stat(bound); err == nil || deletedAt == nil {
		return bound, nil
	}
	legacy, err := legacyDeletedDir(repoPath, *deletedAt)
	if err != nil || legacy == "" {
		return bound, err
	}
	return legacy, nil
}

type dirMove struct{ from, to string }

// moveDirs renames each existing from to its to, never over an existing to.
// On failure it moves back what it already moved, so a repo and its wiki stay
// together.
func moveDirs(moves ...dirMove) error {
	var done []dirMove
	undo := func() {
		for _, m := range slices.Backward(done) {
			_ = os.Rename(m.to, m.from)
		}
	}
	for _, m := range moves {
		if _, err := os.Stat(m.from); err != nil {
			continue
		}
		if _, err := os.Stat(m.to); err == nil {
			undo()
			return fmt.Errorf("%s already exists", m.to)
		}
		if err := os.Rename(m.from, m.to); err != nil {
			undo()
			return err
		}
		done = append(done, m)
	}
	return nil
}
