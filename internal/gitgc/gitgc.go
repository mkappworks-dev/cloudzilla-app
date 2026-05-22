// Package gitgc prunes unreferenced loose objects from bare git
// repositories.
//
// receive-pack writes objects loose (see internal/gittransport) and the
// server has no other reclaim step, so a rejected or churny push leaves
// orphans behind. Prune is that reclaim step: it walks every ref for
// reachability and removes loose objects that are both unreachable and
// older than a grace period. The grace period avoids racing a push that
// has written its objects but not yet updated its ref.
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
	// Grace is the minimum age an unreferenced loose object must reach
	// before it becomes eligible for removal.
	Grace time.Duration
	// DryRun reports what would be pruned without deleting anything.
	DryRun bool
}

// Result summarises a single repository's Prune run. When DryRun is set,
// Pruned and Reclaimed report what would have been removed.
type Result struct {
	Repo      string
	Scanned   int   // loose objects examined
	Pruned    int   // unreferenced, past-grace loose objects removed
	Kept      int   // unreferenced loose objects skipped, still within grace
	Reclaimed int64 // bytes freed
}

// Prune removes unreferenced loose objects from the bare repository at
// repoPath. Reachability is computed from every ref; any error walking
// it aborts the run before a single object is removed.
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

	reachable, err := revlist.Objects(repo.Storer, roots, nil)
	if err != nil {
		return res, fmt.Errorf("walk reachability: %w", err)
	}
	keep := make(map[plumbing.Hash]struct{}, len(reachable))
	for _, h := range reachable {
		keep[h] = struct{}{}
	}

	objectsDir := filepath.Join(gitDir(repoPath), "objects")
	cutoff := time.Now().Add(-opts.Grace)

	fanout, err := os.ReadDir(objectsDir)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("read objects dir: %w", err)
	}
	for _, d := range fanout {
		if !d.IsDir() || !isFanoutDir(d.Name()) {
			continue // skip "pack", "info", and anything unexpected
		}
		subDir := filepath.Join(objectsDir, d.Name())
		entries, err := os.ReadDir(subDir)
		if err != nil {
			return res, fmt.Errorf("read %s: %w", subDir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !isObjectFile(e.Name()) {
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

// refRoots returns the deduplicated set of hashes every ref points at.
// Symbolic refs (HEAD) are skipped — their targets are themselves refs.
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

// gitDir resolves the directory holding objects/, handling both bare
// repositories and ones with a .git subdirectory.
func gitDir(repoPath string) string {
	if fi, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil && fi.IsDir() {
		return filepath.Join(repoPath, ".git")
	}
	return repoPath
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
