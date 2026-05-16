package service

import (
	"archive/zip"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitFile writes content to filePath on the given branch as a new commit,
// preserving every other file in the tree. The branch is created when it does
// not yet exist (e.g. the first commit in an empty repo). filePath is
// repo-root-relative and may contain directory segments, which are created as
// needed.
func (s *CodeService) CommitFile(owner, repoName, branch, filePath string, content []byte, authorName, authorEmail, message string) error {
	filePath = strings.Trim(strings.ReplaceAll(filePath, "\\", "/"), "/")
	if filePath == "" {
		return fmt.Errorf("file path is empty")
	}
	segments := strings.Split(filePath, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid file path: %q", filePath)
		}
	}

	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	blobHash, err := writeBlob(repo, content)
	if err != nil {
		return err
	}

	// Parent commit + root tree from the branch tip, if the branch exists.
	var parentHashes []plumbing.Hash
	var baseTree *object.Tree
	branchRef := plumbing.NewBranchReferenceName(branch)
	if ref, refErr := repo.Reference(branchRef, true); refErr == nil {
		parent, cErr := repo.CommitObject(ref.Hash())
		if cErr != nil {
			return fmt.Errorf("resolve branch tip: %w", cErr)
		}
		parentHashes = []plumbing.Hash{parent.Hash}
		if baseTree, cErr = parent.Tree(); cErr != nil {
			return fmt.Errorf("read tree: %w", cErr)
		}
		// Reject a no-op commit when the file already matches.
		if existing, fErr := baseTree.File(filePath); fErr == nil && existing.Hash == blobHash {
			return fmt.Errorf("file is unchanged")
		}
	}

	rootTreeHash, err := insertBlobIntoTree(repo, baseTree, segments, blobHash)
	if err != nil {
		return err
	}

	sig := object.Signature{Name: authorName, Email: authorEmail, When: time.Now()}
	commit := object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      message,
		TreeHash:     rootTreeHash,
		ParentHashes: parentHashes,
	}
	commitObj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		return err
	}
	commitHash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		return err
	}

	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef, commitHash)); err != nil {
		return fmt.Errorf("advance branch: %w", err)
	}
	// Point HEAD at the branch only when it is missing or detached — i.e. the
	// repo had no commits before this one.
	if headRef, hErr := repo.Storer.Reference(plumbing.HEAD); hErr != nil || headRef.Type() == plumbing.HashReference {
		if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branchRef)); err != nil {
			return fmt.Errorf("set symbolic HEAD: %w", err)
		}
	}
	return nil
}

// writeBlob stores content as a git blob and returns its hash.
func writeBlob(repo *gogit.Repository, content []byte) (plumbing.Hash, error) {
	obj := repo.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	obj.SetSize(int64(len(content)))
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write(content); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(obj)
}

// insertBlobIntoTree rebuilds the tree chain so that segments (the last of
// which is the file name) resolves to blobHash, returning the new root tree
// hash. A nil base means an empty starting tree.
func insertBlobIntoTree(repo *gogit.Repository, base *object.Tree, segments []string, blobHash plumbing.Hash) (plumbing.Hash, error) {
	name := segments[0]
	entries := []object.TreeEntry{}
	if base != nil {
		for _, e := range base.Entries {
			if e.Name != name {
				entries = append(entries, e)
			}
		}
	}

	if len(segments) == 1 {
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Regular, Hash: blobHash})
	} else {
		var subTree *object.Tree
		if base != nil {
			if existing, err := base.Tree(name); err == nil {
				subTree = existing
			}
		}
		subHash, err := insertBlobIntoTree(repo, subTree, segments[1:], blobHash)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: subHash})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	tree := object.Tree{Entries: entries}
	obj := repo.Storer.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(obj)
}

// ArchiveZip streams a zip archive of the repository tree at ref into w. Every
// file is prefixed with "<repo>-<ref>/" so the archive expands into one folder.
func (s *CodeService) ArchiveZip(owner, repoName, ref string, w io.Writer) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return fmt.Errorf("resolve ref: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	prefix := repoName + "-" + strings.ReplaceAll(ref, "/", "-") + "/"
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, werr := walker.Next()
		if werr == io.EOF {
			break
		}
		if werr != nil {
			return werr
		}
		if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable {
			continue
		}
		blob, berr := repo.BlobObject(entry.Hash)
		if berr != nil {
			return berr
		}
		fw, ferr := zw.Create(prefix + name)
		if ferr != nil {
			return ferr
		}
		rc, rerr := blob.Reader()
		if rerr != nil {
			return rerr
		}
		if _, cerr := io.Copy(fw, rc); cerr != nil {
			_ = rc.Close()
			return cerr
		}
		_ = rc.Close()
	}
	return zw.Close()
}

// maxFileList caps the recursive file listing fed to the "Go to file" finder.
const maxFileList = 2000

// ListAllFiles returns every file path in the tree at ref, recursively. The
// result is capped at maxFileList entries so the finder stays responsive on
// very large repositories.
func (s *CodeService) ListAllFiles(owner, repoName, ref string) ([]string, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	var files []string
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, werr := walker.Next()
		if werr == io.EOF {
			break
		}
		if werr != nil {
			return nil, werr
		}
		if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable {
			continue
		}
		files = append(files, name)
		if len(files) >= maxFileList {
			break
		}
	}
	return files, nil
}

// CommitCount returns the number of commits reachable from ref. It walks the
// full history, so callers should treat it as best-effort on large repos.
func (s *CodeService) CommitCount(owner, repoName, ref string) (int, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return 0, err
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return 0, err
	}
	iter, err := repo.Log(&gogit.LogOptions{From: commit.Hash})
	if err != nil {
		return 0, err
	}
	defer iter.Close()
	count := 0
	if err = iter.ForEach(func(*object.Commit) error {
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}
