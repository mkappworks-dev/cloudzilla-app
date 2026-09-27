package service

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Every lifecycle step moves both dirs together: a wiki left behind is served
// to the next repo that takes the name.
func repoDirs(root, owner, name string) (gitDir, wikiDir string) {
	base := filepath.Join(root, owner, name)
	return base + ".git", base + ".wiki.git"
}

func deletedSuffix(now time.Time) string {
	return ".deleted." + strconv.FormatInt(now.Unix(), 10)
}

type dirMove struct{ from, to string }

func movesAside(suffix string, dirs ...string) []dirMove {
	moves := make([]dirMove, len(dirs))
	for i, dir := range dirs {
		moves[i] = dirMove{from: dir, to: dir + suffix}
	}
	return moves
}

// renameDirs applies the moves whose source exists, all or nothing. os.Rename
// refuses to replace an existing directory, so no move lands on another repo.
func renameDirs(moves []dirMove) ([]dirMove, error) {
	var done []dirMove
	for _, m := range moves {
		if err := os.Rename(m.from, m.to); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			revertDirs(done)
			return nil, fmt.Errorf("move %s: %w", filepath.Base(m.from), err)
		}
		done = append(done, m)
	}
	return done, nil
}

// A move that cannot be reverted leaves the dir at its aside path, where
// nothing serves it.
func revertDirs(done []dirMove) {
	for i := len(done) - 1; i >= 0; i-- {
		if err := os.Rename(done[i].to, done[i].from); err != nil {
			slog.Error("revert repo dir move failed", "from", done[i].to, "to", done[i].from, "error", err)
		}
	}
}

// Matches soft-deleted copies the way Restore finds them.
func removeDeletedDirs(root, owner, name string) {
	gitDir, wikiDir := repoDirs(root, owner, name)
	for _, dir := range []string{gitDir, wikiDir} {
		pattern := dir + ".deleted.*"
		matches, err := filepath.Glob(pattern)
		if err != nil {
			slog.Warn("glob deleted repo dirs failed", "pattern", pattern, "error", err)
			continue
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil {
				slog.Warn("remove deleted repo dir failed", "path", m, "error", err)
			}
		}
	}
}
