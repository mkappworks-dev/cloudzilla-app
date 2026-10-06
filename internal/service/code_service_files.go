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
	"unicode/utf8"

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

// ErrFileUnchanged means a commit would leave the tree as it was. Handlers
// answer 422.
var ErrFileUnchanged = errors.New("file is unchanged")

// ErrFileChanged means the path on the branch no longer holds the blob an edit
// or delete was based on. Handlers answer 409.
var ErrFileChanged = errors.New("file changed on the branch")

// CleanFilePath returns p with surrounding slashes trimmed and backslashes
// turned into slashes, or ErrInvalidFilePath when git couldn't check it out.
func CleanFilePath(p string) (string, error) {
	p = strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
	if p == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidFilePath)
	}
	if len(p) > maxFilePathBytes {
		return "", ErrFilePathTooLong
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: %q", ErrInvalidFilePath, p)
		}
		if isDotGit(seg) {
			return "", fmt.Errorf("%w: %q is reserved for Git's own data", ErrInvalidFilePath, seg)
		}
	}
	return p, nil
}

// CommitFile commits content to filePath on branch, creating the branch if it
// does not yet exist (e.g. the first commit in an empty repo). It refuses a
// path git couldn't check out with ErrInvalidFilePath.
func (s *CodeService) CommitFile(owner, repoName, branch, filePath string, content []byte, author GitAuthor, message string) error {
	filePath, err := CleanFilePath(filePath)
	if err != nil {
		return err
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
	var parent *object.Commit
	var baseTree *object.Tree
	branchRef := plumbing.NewBranchReferenceName(branch)
	if ref, refErr := repo.Reference(branchRef, true); refErr == nil {
		var cErr error
		if parent, cErr = repo.CommitObject(ref.Hash()); cErr != nil {
			return fmt.Errorf("resolve branch tip: %w", cErr)
		}
		if baseTree, cErr = parent.Tree(); cErr != nil {
			return fmt.Errorf("read tree: %w", cErr)
		}
	}

	rootTreeHash, err := insertEntry(repo, baseTree, "", strings.Split(filePath, "/"), object.TreeEntry{Mode: filemode.Regular, Hash: blobHash}, true)
	if err != nil {
		return err
	}
	if err := commitOnto(repo, branchRef, parent, rootTreeHash, author, message); err != nil {
		return err
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

// IsEditableText reports whether a textarea can round-trip s: it must be
// UTF-8, and hold no CR outside a CRLF, which the HTML parser turns into a
// line break.
func IsEditableText(s string) bool {
	return utf8.ValidString(s) && strings.Count(s, "\r") == strings.Count(s, "\r\n")
}

// BranchFile is a file on a branch's tip, as GetBranchFile reads it.
type BranchFile struct {
	SHA      string
	Mode     filemode.FileMode
	Size     int64
	IsBinary bool
	// Content is set only for a regular or executable text file of at most
	// GetBranchFile's maxContent bytes.
	Content []byte
}

// GetBranchFile reads the file at filePath on branch, which must be a branch:
// any other ref is ErrRefNotFound. A folder or submodule at filePath is
// object.ErrFileNotFound.
func (s *CodeService) GetBranchFile(owner, repoName, branch, filePath string, maxContent int64) (*BranchFile, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	tip, err := branchTipCommit(repo, branch)
	if err != nil {
		return nil, err
	}
	f, err := tip.File(filePath)
	if err != nil {
		return nil, err
	}
	out := &BranchFile{SHA: f.Hash.String(), Mode: f.Mode, Size: f.Size}
	if f.Mode == filemode.Symlink {
		return out, nil
	}
	if out.IsBinary, err = f.IsBinary(); err != nil {
		return nil, err
	}
	if !out.IsBinary && f.Size <= maxContent {
		contents, err := f.Contents()
		if err != nil {
			return nil, err
		}
		out.Content = []byte(contents)
	}
	return out, nil
}

// EditFile commits content at newPath on branch in place of the file at
// oldPath, which must still hold baseSHA (else ErrFileChanged). A different
// newPath renames the file, keeping its mode; one that holds any entry is
// ErrPathCollision. Unlike CommitFile, it never creates the branch.
func (s *CodeService) EditFile(owner, repoName, branch, oldPath, newPath, baseSHA string, content []byte, author GitAuthor, message string) error {
	// A pushed path CleanFilePath would rewrite, such as one with a backslash,
	// must not be renamed by an edit that leaves the path field alone.
	if newPath != oldPath {
		var err error
		if newPath, err = CleanFilePath(newPath); err != nil {
			return err
		}
	}
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	tip, tree, old, err := baseEntry(repo, branch, oldPath, baseSHA, false)
	if err != nil {
		return err
	}
	blobHash, err := writeBlob(repo, content)
	if err != nil {
		return err
	}
	if newPath == oldPath && blobHash == old.Hash {
		return ErrFileUnchanged
	}

	root := tree
	if newPath != oldPath {
		rootHash, err := removeEntry(repo, tree, "", strings.Split(oldPath, "/"))
		if err != nil {
			return err
		}
		if root, err = repo.TreeObject(rootHash); err != nil {
			return fmt.Errorf("read tree: %w", err)
		}
	}
	rootHash, err := insertEntry(repo, root, "", strings.Split(newPath, "/"), object.TreeEntry{Mode: old.Mode, Hash: blobHash}, newPath == oldPath)
	if err != nil {
		return err
	}
	return commitOnto(repo, plumbing.NewBranchReferenceName(branch), tip, rootHash, author, message)
}

// DeleteFile removes the file or symlink at filePath on branch, which must
// still hold baseSHA (else ErrFileChanged), along with the folders that leaves
// empty. Naming a folder or submodule by its own hash is object.ErrFileNotFound. It returns the nearest folder of filePath that remains, "" for the
// root.
func (s *CodeService) DeleteFile(owner, repoName, branch, filePath, baseSHA string, author GitAuthor, message string) (string, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}
	tip, tree, _, err := baseEntry(repo, branch, filePath, baseSHA, true)
	if err != nil {
		return "", err
	}
	rootHash, err := removeEntry(repo, tree, "", strings.Split(filePath, "/"))
	if err != nil {
		return "", err
	}
	if err := commitOnto(repo, plumbing.NewBranchReferenceName(branch), tip, rootHash, author, message); err != nil {
		return "", err
	}
	root, err := repo.TreeObject(rootHash)
	if err != nil {
		return "", fmt.Errorf("read tree: %w", err)
	}
	dir := path.Dir(filePath)
	for dir != "." {
		if _, err := root.Tree(dir); err == nil {
			return dir, nil
		}
		dir = path.Dir(dir)
	}
	return "", nil
}

// branchTipCommit resolves branch, and only a branch, to its tip commit.
func branchTipCommit(repo *gogit.Repository, branch string) (*object.Commit, error) {
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRefNotFound, branch)
	}
	tip, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("resolve branch tip: %w", err)
	}
	return tip, nil
}

// baseEntry reads branch's tip and the entry at filePath, which must be a
// file (or, with symlinkOK, a symlink) holding baseSHA.
func baseEntry(repo *gogit.Repository, branch, filePath, baseSHA string, symlinkOK bool) (*object.Commit, *object.Tree, *object.TreeEntry, error) {
	tip, err := branchTipCommit(repo, branch)
	if err != nil {
		return nil, nil, nil, err
	}
	tree, err := tip.Tree()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read tree: %w", err)
	}
	e, err := tree.FindEntry(filePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %s is gone", ErrFileChanged, filePath)
	}
	if (e.Mode == filemode.Dir || e.Mode == filemode.Submodule) && e.Hash.String() == baseSHA {
		return nil, nil, nil, fmt.Errorf("%w: %s", object.ErrFileNotFound, filePath)
	}
	isFile := e.Mode == filemode.Regular || e.Mode == filemode.Executable || (symlinkOK && e.Mode == filemode.Symlink)
	if !isFile || e.Hash.String() != baseSHA {
		return nil, nil, nil, fmt.Errorf("%w: %s", ErrFileChanged, filePath)
	}
	return tip, tree, e, nil
}

// commitOnto commits tree onto branch with parent (nil for a branch's first
// commit), moving the branch only if it still points at parent.
func commitOnto(repo *gogit.Repository, branchRef plumbing.ReferenceName, parent *object.Commit, tree plumbing.Hash, author GitAuthor, message string) error {
	var oldTip plumbing.Hash
	var parentHashes []plumbing.Hash
	if parent != nil {
		oldTip = parent.Hash
		parentHashes = []plumbing.Hash{parent.Hash}
	}
	sig := author.signature(time.Now())
	commit := object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      message,
		TreeHash:     tree,
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

// insertEntry rebuilds the tree chain so segments resolve to leaf, returning
// the new root tree hash. A nil base starts from an empty tree, and dir is
// base's path. With replace, a file already at the path is replaced and keeps
// its mode; any other entry in the way is an ErrPathCollision.
func insertEntry(repo *gogit.Repository, base *object.Tree, dir string, segments []string, leaf object.TreeEntry, replace bool) (plumbing.Hash, error) {
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
		leaf.Name = name
		if existing != nil {
			if !replace || (existing.Mode != filemode.Regular && existing.Mode != filemode.Executable) {
				return plumbing.ZeroHash, pathCollision(entryPath, existing.Mode)
			}
			if existing.Hash == leaf.Hash {
				return plumbing.ZeroHash, ErrFileUnchanged
			}
			leaf.Mode = existing.Mode
		}
		entries = append(entries, leaf)
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
		subHash, err := insertEntry(repo, subTree, entryPath, segments[1:], leaf, replace)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: subHash})
	}

	return writeTree(repo, entries)
}

// removeEntry rebuilds the tree chain without the entry at segments, which
// must exist, dropping every folder that leaves empty except the root.
func removeEntry(repo *gogit.Repository, base *object.Tree, dir string, segments []string) (plumbing.Hash, error) {
	name := segments[0]
	entries := []object.TreeEntry{}
	for _, e := range base.Entries {
		if e.Name != name {
			entries = append(entries, e)
			continue
		}
		if len(segments) == 1 {
			continue
		}
		sub, err := repo.TreeObject(e.Hash)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("read %s: %w", path.Join(dir, name), err)
		}
		subHash, err := removeEntry(repo, sub, path.Join(dir, name), segments[1:])
		if err != nil {
			return plumbing.ZeroHash, err
		}
		if subHash != plumbing.ZeroHash {
			entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: subHash})
		}
	}
	if len(entries) == 0 && dir != "" {
		return plumbing.ZeroHash, nil
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
