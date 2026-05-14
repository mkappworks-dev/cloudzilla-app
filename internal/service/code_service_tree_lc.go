package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// TreeEntryWithLastCommit is a directory listing entry enriched with the
// most recent commit that touched it. Used by the tree page. Coexists with
// the existing simple TreeEntry in code_service_tree.go.
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

const treeCacheTTL = 60 * time.Second

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
		last, err := s.lastCommitTouching(repo, commit, e.Path)
		if err != nil {
			slog.Warn("tree last-commit lookup failed",
				"owner", owner,
				"repo", repoName,
				"ref", ref,
				"path", e.Path,
				"error", err,
			)
		} else if last != nil {
			e.LastCommit.SHA = last.Hash.String()
			e.LastCommit.Message = firstLine(last.Message)
			e.LastCommit.Author = last.Author.Name
			e.LastCommit.Timestamp = last.Author.When
		}
		if !e.IsDir {
			if blob, err := repo.BlobObject(entry.Hash); err == nil {
				e.Size = blob.Size
			}
		}
		out = append(out, e)
	}

	s.treeCache.Store(key, treeCacheEntry{entries: out, cachedAt: time.Now()})
	return out, nil
}

// LastCommitForPath returns the most recent commit that touched `path`
// (file or directory) reachable from `ref`. Used by the blob page to render
// the latest-commit sub-header row for a single file.
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
	return s.lastCommitTouching(repo, commit, path)
}

// lastCommitTouching walks the history from headCommit and returns the most
// recent commit whose tree changed `path`. Uses go-git's PathFilter to limit
// iteration to commits that altered files at or under `path`.
func (s *CodeService) lastCommitTouching(repo *gogit.Repository, headCommit *object.Commit, path string) (*object.Commit, error) {
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

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
