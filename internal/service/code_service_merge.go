package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// ErrNoCommonAncestor is returned by findMergeBase when two commits share no
// merge base. Exported so callers can match with errors.Is rather than
// string-comparing the message.
var ErrNoCommonAncestor = errors.New("no common ancestor")

// PRDiffResult holds the diff between two branches and merge capability flags.
type PRDiffResult struct {
	Files            []FileDiff
	TotalAdded       int
	TotalDeleted     int
	CanFastForward   bool // head is a descendant of base
	CanThreeWayMerge bool // branches diverged but no conflicting file edits
}

// mergeFile holds the blob hash and file mode for a single file in a tree.
type mergeFile struct {
	hash plumbing.Hash
	mode filemode.FileMode
}

// checkFastForward returns true if headCommit is a descendant of baseCommit.
func checkFastForward(repo *gogit.Repository, baseCommit, headCommit *object.Commit) bool {
	iter, err := repo.Log(&gogit.LogOptions{From: headCommit.Hash})
	if err != nil {
		return false
	}
	defer iter.Close()
	found := false
	_ = iter.ForEach(func(c *object.Commit) error {
		if c.Hash == baseCommit.Hash {
			found = true
			return storer.ErrStop
		}
		return nil
	})
	return found
}

// GetPullDiff returns the diff between base and head branches, plus whether FF merge is possible.
func (s *CodeService) GetPullDiff(owner, repoName, base, head string) (*PRDiffResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return nil, err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return nil, err
	}

	canFF := checkFastForward(repo, baseCommit, headCommit)
	var canMerge3 bool
	if !canFF {
		mb, mbErr := findMergeBase(repo, baseCommit, headCommit)
		if mbErr == nil {
			_, canMerge3, _ = mergeTreesNoConflict(repo, mb, baseCommit, headCommit)
		}
	}

	patch, err := baseCommit.Patch(headCommit)
	if err != nil {
		return nil, err
	}

	var files []FileDiff
	totalAdded, totalDeleted := 0, 0

	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		var oldPath, newPath string
		if from != nil {
			oldPath = from.Path()
		}
		if to != nil {
			newPath = to.Path()
		}
		isNew := from == nil
		isDelete := to == nil
		if isDelete && newPath == "" {
			newPath = oldPath
		}
		if isNew && oldPath == "" {
			oldPath = newPath
		}

		fd := FileDiff{
			OldPath:  oldPath,
			NewPath:  newPath,
			IsBinary: fp.IsBinary(),
			IsNew:    isNew,
			IsDelete: isDelete,
		}
		if !fp.IsBinary() {
			fd.Hunks = buildHunks(fp.Chunks())
			for _, h := range fd.Hunks {
				for _, l := range h.Lines {
					switch l.Type {
					case "add":
						fd.Added++
					case "del":
						fd.Deleted++
					}
				}
			}
		}
		totalAdded += fd.Added
		totalDeleted += fd.Deleted
		files = append(files, fd)
	}

	return &PRDiffResult{
		Files:            files,
		TotalAdded:       totalAdded,
		TotalDeleted:     totalDeleted,
		CanFastForward:   canFF,
		CanThreeWayMerge: canMerge3,
	}, nil
}

// MergePullRequest performs a fast-forward merge of head into base.
func (s *CodeService) MergePullRequest(owner, repoName, base, head string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return err
	}

	if !checkFastForward(repo, baseCommit, headCommit) {
		return errors.New("cannot merge: branches have diverged (fast-forward not possible)")
	}

	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(base), headCommit.Hash)
	return repo.Storer.SetReference(ref)
}

// flattenTree walks a git tree and returns a flat map of full path → mergeFile.
func flattenTree(tree *object.Tree) (map[string]mergeFile, error) {
	result := make(map[string]mergeFile)
	iter := tree.Files()
	err := iter.ForEach(func(f *object.File) error {
		result[f.Name] = mergeFile{hash: f.Blob.Hash, mode: f.Mode}
		return nil
	})
	return result, err
}

// findMergeBase returns the most recent common ancestor of commits a and b.
func findMergeBase(repo *gogit.Repository, a, b *object.Commit) (*object.Commit, error) {
	aAncestors := make(map[plumbing.Hash]bool)
	iterA, err := repo.Log(&gogit.LogOptions{From: a.Hash})
	if err != nil {
		return nil, err
	}
	defer iterA.Close()
	_ = iterA.ForEach(func(c *object.Commit) error {
		aAncestors[c.Hash] = true
		return nil
	})

	iterB, err := repo.Log(&gogit.LogOptions{From: b.Hash})
	if err != nil {
		return nil, err
	}
	defer iterB.Close()
	var base *object.Commit
	_ = iterB.ForEach(func(c *object.Commit) error {
		if aAncestors[c.Hash] {
			base = c
			return storer.ErrStop
		}
		return nil
	})
	if base == nil {
		return nil, ErrNoCommonAncestor
	}
	return base, nil
}

// buildTree recursively encodes a flat file map into git tree objects and returns the root tree hash.
func buildTree(repo *gogit.Repository, files map[string]mergeFile) (plumbing.Hash, error) {
	// Sort paths for deterministic tree encoding.
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Collect direct children and subdirectory groups.
	subdirs := make(map[string]map[string]mergeFile) // dir name → sub-map
	var entries []object.TreeEntry

	for _, p := range paths {
		mf := files[p]
		slash := strings.Index(p, "/")
		if slash == -1 {
			// Blob entry.
			entries = append(entries, object.TreeEntry{
				Name: p,
				Mode: mf.mode,
				Hash: mf.hash,
			})
		} else {
			dir := p[:slash]
			rest := p[slash+1:]
			if subdirs[dir] == nil {
				subdirs[dir] = make(map[string]mergeFile)
			}
			subdirs[dir][rest] = mf
		}
	}

	// Gather subdir names sorted.
	subdirNames := make([]string, 0, len(subdirs))
	for d := range subdirs {
		subdirNames = append(subdirNames, d)
	}
	sort.Strings(subdirNames)

	// Build subtree entries first so the final list can be sorted blobs + subtrees.
	var subtreeEntries []object.TreeEntry
	for _, dir := range subdirNames {
		subHash, err := buildTree(repo, subdirs[dir])
		if err != nil {
			return plumbing.ZeroHash, err
		}
		subtreeEntries = append(subtreeEntries, object.TreeEntry{
			Name: dir,
			Mode: filemode.Dir,
			Hash: subHash,
		})
	}

	// Merge and sort all entries lexicographically (git requirement).
	all := append(subtreeEntries, entries...)
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	tree := &object.Tree{Entries: all}
	obj := repo.Storer.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	h, err := repo.Storer.SetEncodedObject(obj)
	return h, err
}

// mergeTreesNoConflict performs a three-way merge of the file trees.
// Returns the merged tree hash, true if no conflicts, and any error.
func mergeTreesNoConflict(repo *gogit.Repository, mergeBase, base, head *object.Commit) (plumbing.Hash, bool, error) {
	mbTree, err := mergeBase.Tree()
	if err != nil {
		return plumbing.ZeroHash, false, err
	}
	baseTree, err := base.Tree()
	if err != nil {
		return plumbing.ZeroHash, false, err
	}
	headTree, err := head.Tree()
	if err != nil {
		return plumbing.ZeroHash, false, err
	}

	mbFiles, err := flattenTree(mbTree)
	if err != nil {
		return plumbing.ZeroHash, false, err
	}
	baseFiles, err := flattenTree(baseTree)
	if err != nil {
		return plumbing.ZeroHash, false, err
	}
	headFiles, err := flattenTree(headTree)
	if err != nil {
		return plumbing.ZeroHash, false, err
	}

	// Compute which paths changed in head relative to merge base.
	headChanges := make(map[string]bool)
	for p, mf := range headFiles {
		if mbMF, ok := mbFiles[p]; !ok || mbMF.hash != mf.hash {
			headChanges[p] = true
		}
	}
	for p := range mbFiles {
		if _, ok := headFiles[p]; !ok {
			headChanges[p] = true // deleted in head
		}
	}

	// Compute which paths changed in base relative to merge base.
	baseChanges := make(map[string]bool)
	for p, mf := range baseFiles {
		if mbMF, ok := mbFiles[p]; !ok || mbMF.hash != mf.hash {
			baseChanges[p] = true
		}
	}
	for p := range mbFiles {
		if _, ok := baseFiles[p]; !ok {
			baseChanges[p] = true // deleted in base
		}
	}

	// Conflict: same path modified in both sides.
	for p := range headChanges {
		if baseChanges[p] {
			return plumbing.ZeroHash, false, nil
		}
	}

	// Apply head changes onto base file set.
	merged := make(map[string]mergeFile, len(baseFiles))
	for p, mf := range baseFiles {
		merged[p] = mf
	}
	for p := range headChanges {
		if mf, ok := headFiles[p]; ok {
			merged[p] = mf
		} else {
			delete(merged, p) // deleted in head
		}
	}

	hash, err := buildTree(repo, merged)
	return hash, err == nil, err
}

// ThreeWayMergePullRequest creates a merge commit combining head into base.
func (s *CodeService) ThreeWayMergePullRequest(owner, repoName, base, head, authorName, authorEmail string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return err
	}

	// If FF is possible, just advance the ref.
	if checkFastForward(repo, baseCommit, headCommit) {
		ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(base), headCommit.Hash)
		return repo.Storer.SetReference(ref)
	}

	mb, err := findMergeBase(repo, baseCommit, headCommit)
	if err != nil {
		return fmt.Errorf("cannot find merge base: %w", err)
	}
	mergedTreeHash, ok, err := mergeTreesNoConflict(repo, mb, baseCommit, headCommit)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("cannot merge: conflicting changes in both branches")
	}

	now := time.Now()
	sig := object.Signature{Name: authorName, Email: authorEmail, When: now}
	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      "Merge branch '" + head + "' into '" + base + "'",
		TreeHash:     mergedTreeHash,
		ParentHashes: []plumbing.Hash{baseCommit.Hash, headCommit.Hash},
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return err
	}
	h, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return err
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(base), h)
	return repo.Storer.SetReference(ref)
}

// SquashMergePullRequest creates a single squash commit on base incorporating all head changes.
func (s *CodeService) SquashMergePullRequest(owner, repoName, base, head, authorName, authorEmail string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return err
	}

	var treeHash plumbing.Hash
	if checkFastForward(repo, baseCommit, headCommit) {
		// FF case: squash commit uses head's tree directly.
		treeHash = headCommit.TreeHash
	} else {
		mb, err := findMergeBase(repo, baseCommit, headCommit)
		if err != nil {
			return fmt.Errorf("cannot find merge base: %w", err)
		}
		mergedTreeHash, ok, err := mergeTreesNoConflict(repo, mb, baseCommit, headCommit)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("cannot merge: conflicting changes in both branches")
		}
		treeHash = mergedTreeHash
	}

	now := time.Now()
	sig := object.Signature{Name: authorName, Email: authorEmail, When: now}
	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      "Squash merge branch '" + head + "' into '" + base + "'",
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{baseCommit.Hash},
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return err
	}
	h, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return err
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(base), h)
	return repo.Storer.SetReference(ref)
}

// ApplySuggestion replaces targetLine (1-based) in filePath on branch with the replacement
// text and creates a new commit on that branch.
func (s *CodeService) ApplySuggestion(owner, repoName, branch, filePath string, targetLine int, replacement, authorName, authorEmail string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	headCommit, _, err := resolveRef(repo, branch)
	if err != nil {
		return err
	}

	// Read current file content.
	raw, err := s.GetRawBlob(owner, repoName, branch, filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	lines := strings.Split(string(raw), "\n")
	if targetLine < 1 || targetLine > len(lines) {
		return fmt.Errorf("line %d out of range (file has %d lines)", targetLine, len(lines))
	}
	// Replace the target line with the suggestion content.
	replacementLines := strings.Split(replacement, "\n")
	updated := make([]string, 0, len(lines)+len(replacementLines)-1)
	updated = append(updated, lines[:targetLine-1]...)
	updated = append(updated, replacementLines...)
	updated = append(updated, lines[targetLine:]...)
	newContent := strings.Join(updated, "\n")

	// Write the new blob.
	blobObj := repo.Storer.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	w, err := blobObj.Writer()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(newContent)); err != nil {
		return err
	}
	_ = w.Close()
	blobHash, err := repo.Storer.SetEncodedObject(blobObj)
	if err != nil {
		return err
	}

	// Rebuild the tree with the updated file.
	tree, err := headCommit.Tree()
	if err != nil {
		return err
	}
	files, err := flattenTree(tree)
	if err != nil {
		return err
	}
	existing, ok := files[filePath]
	if !ok {
		existing = mergeFile{mode: filemode.Regular}
	}
	files[filePath] = mergeFile{hash: blobHash, mode: existing.mode}
	newTreeHash, err := buildTree(repo, files)
	if err != nil {
		return err
	}

	// Create new commit.
	now := time.Now()
	sig := object.Signature{Name: authorName, Email: authorEmail, When: now}
	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      "Apply suggestion to " + filePath,
		TreeHash:     newTreeHash,
		ParentHashes: []plumbing.Hash{headCommit.Hash},
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return err
	}
	newHash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		return err
	}

	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), newHash)
	return repo.Storer.SetReference(ref)
}
