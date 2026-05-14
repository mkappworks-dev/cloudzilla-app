package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// TreeEntryWithLastCommit is a directory listing entry enriched with the
// most recent commit that touched it. Used by the tree page.
type TreeEntryWithLastCommit struct {
	Name       string
	Path       string
	IsDir      bool
	Size       int64
	LastCommit struct {
		SHA       string
		Message   string
		Author    string
		Timestamp time.Time
	}
}

type treeCacheEntry struct {
	entries  []TreeEntryWithLastCommit
	cachedAt time.Time
}

const (
	treeCacheTTL = 60 * time.Second
	// treeCacheMaxKeys: insert counter at which the eviction sweep fires.
	// Caps cache memory under unbounded SHA enumeration.
	treeCacheMaxKeys = 1024
)

// ListEntriesWithLastCommit returns the entries at `dir` enriched with the
// most recent commit that touched each entry. Results are cached per
// (owner, repo, ref, dir) for treeCacheTTL.
func (s *CodeService) ListEntriesWithLastCommit(ctx context.Context, owner, repoName, ref, dir string) ([]TreeEntryWithLastCommit, error) {
	// resolveRef treats "" as HEAD; the literal "HEAD" would be tried as a branch/tag/SHA and fail.
	if ref == "HEAD" {
		ref = ""
	}

	key := owner + "/" + repoName + ":" + ref + ":" + dir
	if v, ok := s.treeCache.Load(key); ok {
		e := v.(treeCacheEntry)
		if time.Since(e.cachedAt) < treeCacheTTL {
			return e.entries, nil
		}
	}

	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}
	rootTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	tree := rootTree
	if dir != "" {
		tree, err = rootTree.Tree(dir)
		if err != nil {
			return nil, err
		}
	}

	out := make([]TreeEntryWithLastCommit, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		e := TreeEntryWithLastCommit{
			Name:  entry.Name,
			Path:  joinPath(dir, entry.Name),
			IsDir: entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule,
		}
		last, err := s.lastCommitTouching(ctx, repo, commit, e.Path)
		if err != nil {
			// Cancellation must propagate; otherwise partial entries ship with empty LastCommit.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			slog.Warn("tree last-commit lookup failed",
				"owner", owner,
				"repo", repoName,
				"ref", ref,
				"path", e.Path,
				"error", err,
			)
		} else if last != nil {
			e.LastCommit.SHA = last.Hash.String()
			e.LastCommit.Message = FirstLine(last.Message)
			e.LastCommit.Author = last.Author.Name
			e.LastCommit.Timestamp = last.Author.When
		}
		if !e.IsDir {
			blob, err := repo.BlobObject(entry.Hash)
			if err != nil {
				slog.Warn("tree blob size lookup failed",
					"owner", owner, "repo", repoName, "ref", ref, "path", e.Path,
					"hash", entry.Hash.String(), "error", err)
			} else {
				e.Size = blob.Size
			}
		}
		out = append(out, e)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})

	s.cacheTreeEntries(key, out)
	return out, nil
}

// cacheTreeEntries stores entries and triggers an expired-entry sweep once
// inserts cross treeCacheMaxKeys. Best-effort counter — sync.Map has no Len.
func (s *CodeService) cacheTreeEntries(key string, entries []TreeEntryWithLastCommit) {
	s.treeCache.Store(key, treeCacheEntry{entries: entries, cachedAt: time.Now()})
	if atomic.AddInt64(&s.treeCacheKeys, 1) > treeCacheMaxKeys {
		atomic.StoreInt64(&s.treeCacheKeys, 0)
		now := time.Now()
		s.treeCache.Range(func(k, v any) bool {
			e, ok := v.(treeCacheEntry)
			if !ok {
				slog.Warn("treeCache: unexpected value type during sweep", "key", k)
				return true
			}
			if now.Sub(e.cachedAt) >= treeCacheTTL {
				s.treeCache.Delete(k)
			}
			return true
		})
	}
}

// LastCommitForPath returns the most recent commit that touched path reachable from ref.
func (s *CodeService) LastCommitForPath(ctx context.Context, owner, repoName, ref, path string) (*object.Commit, error) {
	if ref == "HEAD" {
		ref = ""
	}
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return s.lastCommitTouching(ctx, repo, commit, path)
}

// lastCommitTouching returns the most recent commit whose tree changed path,
// walked from headCommit via PathFilter. Aborts on ctx cancellation.
func (s *CodeService) lastCommitTouching(ctx context.Context, repo *gogit.Repository, headCommit *object.Commit, path string) (*object.Commit, error) {
	prefix := path
	if prefix != "" {
		prefix = prefix + "/"
	}
	iter, err := repo.Log(&gogit.LogOptions{
		From: headCommit.Hash,
		PathFilter: func(p string) bool {
			if p == path {
				return true
			}
			return prefix != "" && strings.HasPrefix(p, prefix)
		},
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var found *object.Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		found = c
		return storer.ErrStop
	})
	if err != nil && err != storer.ErrStop {
		return nil, err
	}
	return found, nil
}

func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// FirstLine returns the subject line (first \n-terminated segment) of s.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
