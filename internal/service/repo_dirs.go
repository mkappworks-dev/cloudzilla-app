package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrRepoNameTaken also covers a directory on disk with no row, so a create
// cannot tell the two apart.
var ErrRepoNameTaken = errors.New("a repository with that name already exists")

// Every lifecycle step moves both dirs together: a wiki left behind is served
// to the next repo that takes the name.
func repoDirs(root, owner, name string) (gitDir, wikiDir string) {
	base := filepath.Join(root, owner, name)
	return base + ".git", base + ".wiki.git"
}

// A path that cannot be checked counts as taken.
func pathTaken(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// claimRepo reserves owner/name for a new row. os.Mkdir fails on an existing
// path, so the row never adopts data an earlier holder of the name left on
// disk; the wiki path is checked too because wikis are created lazily.
func claimRepo(ctx context.Context, repos *store.RepoStore, root, owner, name string) (string, error) {
	if _, err := repos.GetByOwnerName(ctx, owner, name); err == nil {
		return "", ErrRepoNameTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	gitDir, wikiDir := repoDirs(root, owner, name)
	if err := os.MkdirAll(filepath.Dir(gitDir), 0o755); err != nil {
		return "", fmt.Errorf("create owner dir: %w", err)
	}
	if pathTaken(wikiDir) {
		return "", ErrRepoNameTaken
	}
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ErrRepoNameTaken
		}
		return "", fmt.Errorf("create repo dir: %w", err)
	}
	return gitDir, nil
}

// Handlers show ErrRepoNameTaken's message, so it is returned unwrapped.
func repoNameErr(op string, err error) error {
	if errors.Is(err, store.ErrRepoNameInUse) {
		return ErrRepoNameTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}

// abandonNewRepo undoes a create that failed after claimRepo. It ignores the
// request's cancellation: a row left behind would outlive its directory.
func abandonNewRepo(ctx context.Context, repos *store.RepoStore, repoID int64, gitDir string) {
	if repoID != 0 {
		if err := repos.DeleteByID(context.WithoutCancel(ctx), repoID); err != nil {
			slog.Error("delete row of failed repo create", "repo_id", repoID, "error", err)
		}
	}
	if err := os.RemoveAll(gitDir); err != nil {
		slog.Error("remove dir of failed repo create", "path", gitDir, "error", err)
	}
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
