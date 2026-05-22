// Package gitgc prunes unreferenced loose objects from bare git
// repositories. receive-pack writes objects loose and the server has no
// other reclaim step, so rejected or churny pushes leave orphans behind.
package gitgc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/revlist"
)

// Options controls a Prune run.
type Options struct {
	// Grace spares unreferenced objects younger than this, avoiding a
	// race with a push that wrote objects but has not updated its ref.
	Grace  time.Duration
	DryRun bool
}

// Result summarises a Prune run. With DryRun set, Pruned and Reclaimed
// report what would have been removed.
type Result struct {
	Repo      string
	Scanned   int   // loose objects examined
	Pruned    int   // unreferenced past-grace objects removed
	Kept      int   // unreferenced objects still within grace
	Reclaimed int64 // bytes freed
	// Skipped is set when the repo has no ref roots: every loose object
	// would look unreachable, so Prune refuses rather than risk emptying
	// the object database of a repo whose refs were lost.
	Skipped bool
}

// Prune removes unreferenced loose objects from the bare repository at
// repoPath. Any error collecting refs or walking reachability aborts the
// run before a single object is removed.
func Prune(repoPath string, opts Options) (Result, error) {
	res := Result{Repo: repoPath}

	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return res, fmt.Errorf("open repository: %w", err)
	}

	roots, err := refRoots(repo)
	if err != nil {
		return res, fmt.Errorf("collect refs: %w", err)
	}
	if len(roots) == 0 {
		res.Skipped = true
		return res, nil
	}

	reachable, err := revlist.Objects(repo.Storer, roots, nil)
	if err != nil {
		return res, fmt.Errorf("walk reachability: %w", err)
	}
	keep := make(map[plumbing.Hash]struct{}, len(reachable))
	for _, h := range reachable {
		keep[h] = struct{}{}
	}

	objectsDir, err := locateObjectsDir(repoPath)
	if err != nil {
		return res, err
	}
	if objectsDir == "" {
		return res, nil
	}
	cutoff := time.Now().Add(-opts.Grace)

	fanout, err := os.ReadDir(objectsDir)
	if err != nil {
		return res, fmt.Errorf("read objects dir: %w", err)
	}
	for _, d := range fanout {
		// DirEntry.IsDir is false for a symlink, so a symlinked fanout
		// dir is skipped and never descended into.
		if !d.IsDir() || !isFanoutDir(d.Name()) {
			continue
		}
		subDir := filepath.Join(objectsDir, d.Name())
		entries, err := os.ReadDir(subDir)
		if err != nil {
			return res, fmt.Errorf("read %s: %w", subDir, err)
		}
		for _, e := range entries {
			// Loose objects are always regular files; skipping symlinks
			// keeps the run from being steered into deleting elsewhere.
			if !e.Type().IsRegular() || !isObjectFile(e.Name()) {
				continue
			}
			h := plumbing.NewHash(d.Name() + e.Name())
			res.Scanned++
			if _, ok := keep[h]; ok {
				continue
			}
			info, err := e.Info()
			if err != nil {
				return res, fmt.Errorf("stat object %s: %w", h, err)
			}
			if info.ModTime().After(cutoff) {
				res.Kept++
				continue
			}
			if !opts.DryRun {
				if err := os.Remove(filepath.Join(subDir, e.Name())); err != nil {
					return res, fmt.Errorf("remove object %s: %w", h, err)
				}
			}
			res.Pruned++
			res.Reclaimed += info.Size()
		}
	}
	return res, nil
}

// refRoots returns the deduplicated hashes every ref points at. Symbolic
// refs (HEAD) are skipped — their targets are refs in their own right.
func refRoots(repo *gogit.Repository) ([]plumbing.Hash, error) {
	iter, err := repo.References()
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	seen := make(map[plumbing.Hash]struct{})
	var roots []plumbing.Hash
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}
		h := ref.Hash()
		if h == plumbing.ZeroHash {
			return nil
		}
		if _, ok := seen[h]; !ok {
			seen[h] = struct{}{}
			roots = append(roots, h)
		}
		return nil
	})
	return roots, err
}

// locateObjectsDir returns the objects directory of the repo at
// repoPath, or "" if it does not exist. A symlinked git dir or objects
// dir is rejected: the GC deletes files and must not be redirected
// outside the repository.
func locateObjectsDir(repoPath string) (string, error) {
	gitDir := repoPath
	if fi, err := os.Lstat(filepath.Join(repoPath, ".git")); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New(".git is a symlink; refusing to prune")
		}
		if fi.IsDir() {
			gitDir = filepath.Join(repoPath, ".git")
		}
	}

	objects := filepath.Join(gitDir, "objects")
	fi, err := os.Lstat(objects)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("stat objects dir: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("objects is a symlink; refusing to prune")
	}
	return objects, nil
}

func isFanoutDir(name string) bool  { return len(name) == 2 && isHex(name) }
func isObjectFile(name string) bool { return len(name) == 38 && isHex(name) }

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
