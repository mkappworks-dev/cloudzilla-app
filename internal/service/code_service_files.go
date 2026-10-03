package service

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
)

// ErrPathCollision means a commit's path runs into an existing entry of another
// kind, such as a file where the path needs a directory. Handlers answer 409.
var ErrPathCollision = errors.New("path collides with an existing entry")

// maxFilePathBytes is Linux's PATH_MAX: no checkout there could hold a longer
// path. It also bounds CommitFile's tree work, which grows with path depth.
const maxFilePathBytes = 4096

// ErrInvalidFilePath means git couldn't check out a commit's path, as with
// ErrFilePathTooLong. Handlers answer 422.
var (
	ErrInvalidFilePath = errors.New("invalid file path")
	ErrFilePathTooLong = fmt.Errorf("%w: longer than %d bytes", ErrInvalidFilePath, maxFilePathBytes)
)

// CommitFile commits content to filePath on branch, creating the branch if it
// does not yet exist (e.g. the first commit in an empty repo). It refuses a
// path git couldn't check out with ErrInvalidFilePath.
func (s *CodeService) CommitFile(owner, repoName, branch, filePath string, content []byte, author GitAuthor, message string) error {
	filePath = strings.Trim(strings.ReplaceAll(filePath, "\\", "/"), "/")
	if filePath == "" {
		return fmt.Errorf("%w: empty", ErrInvalidFilePath)
	}
	if len(filePath) > maxFilePathBytes {
		return ErrFilePathTooLong
	}
	segments := strings.Split(filePath, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q", ErrInvalidFilePath, filePath)
		}
		if isDotGit(seg) {
			return fmt.Errorf("%w: %q is reserved for Git's own data", ErrInvalidFilePath, seg)
		}
	}

	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	blobHash, err := writeBlob(repo, content)
	if err != nil {
		return err
	}

	// Parent commit + root tree from the branch tip, if the branch exists.
	var oldTip plumbing.Hash
	var parentHashes []plumbing.Hash
	var baseTree *object.Tree
	branchRef := plumbing.NewBranchReferenceName(branch)
	if ref, refErr := repo.Reference(branchRef, true); refErr == nil {
		parent, cErr := repo.CommitObject(ref.Hash())
		if cErr != nil {
			return fmt.Errorf("resolve branch tip: %w", cErr)
		}
		oldTip = parent.Hash
		parentHashes = []plumbing.Hash{parent.Hash}
		if baseTree, cErr = parent.Tree(); cErr != nil {
			return fmt.Errorf("read tree: %w", cErr)
		}
	}

	rootTreeHash, err := insertBlobIntoTree(repo, baseTree, "", segments, blobHash)
	if err != nil {
		return err
	}

	sig := author.signature(time.Now())
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

	if err := gitref.Move(repo.Storer, branchRef, oldTip, commitHash); err != nil {
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

// writeTree stores entries as a tree in the order git requires, where a
// directory sorts as if its name ended in "/": docs.md before docs/.
func writeTree(repo *gogit.Repository, entries []object.TreeEntry) (plumbing.Hash, error) {
	sort.Sort(object.TreeEntrySorter(entries))
	obj := repo.Storer.NewEncodedObject()
	if err := (&object.Tree{Entries: entries}).Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return repo.Storer.SetEncodedObject(obj)
}

// insertBlobIntoTree rebuilds the tree chain so segments resolve to blobHash,
// returning the new root tree hash. A nil base starts from an empty tree, and
// dir is base's path. Only a file is replaced, keeping its mode; any other
// entry in the way is an ErrPathCollision.
func insertBlobIntoTree(repo *gogit.Repository, base *object.Tree, dir string, segments []string, blobHash plumbing.Hash) (plumbing.Hash, error) {
	name := segments[0]
	entryPath := path.Join(dir, name)
	var existing *object.TreeEntry
	entries := []object.TreeEntry{}
	if base != nil {
		for i, e := range base.Entries {
			if e.Name == name {
				existing = &base.Entries[i]
			} else {
				entries = append(entries, e)
			}
		}
	}

	if len(segments) == 1 {
		mode := filemode.Regular
		if existing != nil {
			if existing.Mode != filemode.Regular && existing.Mode != filemode.Executable {
				return plumbing.ZeroHash, pathCollision(entryPath, existing.Mode)
			}
			if existing.Hash == blobHash {
				return plumbing.ZeroHash, errors.New("file is unchanged")
			}
			mode = existing.Mode
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: mode, Hash: blobHash})
	} else {
		var subTree *object.Tree
		if existing != nil {
			if existing.Mode != filemode.Dir {
				return plumbing.ZeroHash, pathCollision(entryPath, existing.Mode)
			}
			var err error
			if subTree, err = repo.TreeObject(existing.Hash); err != nil {
				return plumbing.ZeroHash, fmt.Errorf("read %s: %w", entryPath, err)
			}
		}
		subHash, err := insertBlobIntoTree(repo, subTree, entryPath, segments[1:], blobHash)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: subHash})
	}

	return writeTree(repo, entries)
}

func pathCollision(p string, mode filemode.FileMode) error {
	kind := "file"
	switch mode {
	case filemode.Dir:
		kind = "directory"
	case filemode.Symlink:
		kind = "symlink"
	case filemode.Submodule:
		kind = "submodule"
	}
	return fmt.Errorf("%w: %s is a %s", ErrPathCollision, p, kind)
}

// ArchiveZip streams a zip of the repo tree at ref into w. Files are prefixed
// "<repo>-<ref>/" so the archive expands into a single folder.
func (s *CodeService) ArchiveZip(owner, repoName, ref string, w io.Writer) error {
	repo, err := s.openRepo(owner, repoName)
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

// ListAllFiles returns every file path in the tree at ref, capped at
// maxFileList so the "Go to file" finder stays responsive on large repos.
func (s *CodeService) ListAllFiles(owner, repoName, ref string) ([]string, error) {
	repo, err := s.openRepo(owner, repoName)
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

// CommitCount returns the commit count reachable from ref. It walks the full
// history — treat as best-effort on large repos.
func (s *CodeService) CommitCount(owner, repoName, ref string) (int, error) {
	repo, err := s.openRepo(owner, repoName)
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
