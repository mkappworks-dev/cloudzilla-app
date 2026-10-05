package service

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	gogitdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
)

// Exported so callers can use errors.Is rather than message comparison.
var ErrNoCommonAncestor = errors.New("no common ancestor")

type PRDiffResult struct {
	Files        []FileDiff
	TotalAdded   int
	TotalDeleted int
}

type DiffStats struct {
	Files   int
	Added   int
	Deleted int
}

// mergeFile is a non-directory tree entry. A submodule's hash names a commit in
// the submodule's repo, not an object in this one.
type mergeFile struct {
	hash plumbing.Hash
	mode filemode.FileMode
}

// checkFastForward returns true if headCommit is a descendant of baseCommit.
func checkFastForward(repo *gogit.Repository, baseCommit, headCommit *object.Commit) bool {
	ok, err := isAncestor(repo, baseCommit, headCommit)
	return err == nil && ok
}

// pullPatch diffs head against its merge base with base, so commits that landed
// on base after head branched off don't show as reverted by the PR. Unrelated
// histories have no merge base and diff the tips.
func pullPatch(repo *gogit.Repository, base, head *object.Commit) (*object.Patch, error) {
	mb, err := findMergeBase(repo, base, head)
	if errors.Is(err, ErrNoCommonAncestor) {
		return base.Patch(head)
	}
	if err != nil {
		return nil, err
	}
	return mb.Patch(head)
}

// GetPullDiff returns the diff between base and head branches.
func (s *CodeService) GetPullDiff(owner, repoName, base, head string) (*PRDiffResult, error) {
	repo, err := s.openRepo(owner, repoName)
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

	patch, err := pullPatch(repo, baseCommit, headCommit)
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
		if from != nil {
			fd.oldBlob = from.Hash()
		}
		if to != nil {
			fd.newBlob = to.Hash()
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
		Files:        files,
		TotalAdded:   totalAdded,
		TotalDeleted: totalDeleted,
	}, nil
}

// PullDiffStats returns GetPullDiff's file and line totals without building hunks.
func (s *CodeService) PullDiffStats(owner, repoName, base, head string) (DiffStats, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return DiffStats{}, err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return DiffStats{}, err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return DiffStats{}, err
	}
	patch, err := pullPatch(repo, baseCommit, headCommit)
	if err != nil {
		return DiffStats{}, err
	}

	// Not patch.Stats(): it drops files with no chunks (binary or empty), which
	// GetPullDiff lists.
	var st DiffStats
	for _, fp := range patch.FilePatches() {
		st.Files++
		if fp.IsBinary() {
			continue
		}
		for _, chunk := range fp.Chunks() {
			switch chunk.Type() {
			case gogitdiff.Add:
				st.Added += len(chunkLines(chunk))
			case gogitdiff.Delete:
				st.Deleted += len(chunkLines(chunk))
			}
		}
	}
	return st, nil
}

// MergePullRequest performs a fast-forward merge of head into base.
func (s *CodeService) MergePullRequest(owner, repoName, base, head string) error {
	repo, err := s.openRepo(owner, repoName)
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

	return gitref.Move(repo.Storer, plumbing.NewBranchReferenceName(base), baseCommit.Hash, headCommit.Hash)
}

// flattenTree walks a git tree and returns a flat map of full path → mergeFile.
// Not tree.Files(): it skips submodules, which buildTree would then drop.
func flattenTree(tree *object.Tree) (map[string]mergeFile, error) {
	result := make(map[string]mergeFile)
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		if entry.Mode != filemode.Dir {
			result[name] = mergeFile{hash: entry.Hash, mode: entry.Mode}
		}
	}
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

	return writeTree(repo, append(subtreeEntries, entries...))
}

// changedPaths returns the paths added, deleted, or changed in content or mode
// between from and to.
func changedPaths(from, to map[string]mergeFile) map[string]bool {
	changed := make(map[string]bool)
	for p, mf := range to {
		if old, ok := from[p]; !ok || old != mf {
			changed[p] = true
		}
	}
	for p := range from {
		if _, ok := to[p]; !ok {
			changed[p] = true
		}
	}
	return changed
}

// mergeFiles performs a three-way merge of the file trees in memory.
// Returns the merged files, true if no conflicts, and any error.
func mergeFiles(mergeBase, base, head *object.Commit) (map[string]mergeFile, bool, error) {
	mbTree, err := mergeBase.Tree()
	if err != nil {
		return nil, false, err
	}
	baseTree, err := base.Tree()
	if err != nil {
		return nil, false, err
	}
	headTree, err := head.Tree()
	if err != nil {
		return nil, false, err
	}

	mbFiles, err := flattenTree(mbTree)
	if err != nil {
		return nil, false, err
	}
	baseFiles, err := flattenTree(baseTree)
	if err != nil {
		return nil, false, err
	}
	headFiles, err := flattenTree(headTree)
	if err != nil {
		return nil, false, err
	}

	headChanges := changedPaths(mbFiles, headFiles)
	baseChanges := changedPaths(mbFiles, baseFiles)

	// Conflict: same path modified in both sides.
	for p := range headChanges {
		if baseChanges[p] {
			return nil, false, nil
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

	// Conflict: a non-directory entry on one side where the other side has a directory.
	if hasPathUnderFile(merged) {
		return nil, false, nil
	}
	return merged, true, nil
}

// hasPathUnderFile reports whether a path in files lies below another, as
// lib/x below lib. A tree can't hold both: it would need two entries named lib.
func hasPathUnderFile(files map[string]mergeFile) bool {
	for p := range files {
		for i := range len(p) {
			if p[i] == '/' {
				if _, ok := files[p[:i]]; ok {
					return true
				}
			}
		}
	}
	return false
}

// mergeTreesNoConflict writes mergeFiles' result into repo as a tree.
// Returns the merged tree hash, true if no conflicts, and any error.
func mergeTreesNoConflict(repo *gogit.Repository, mergeBase, base, head *object.Commit) (plumbing.Hash, bool, error) {
	merged, ok, err := mergeFiles(mergeBase, base, head)
	if err != nil || !ok {
		return plumbing.ZeroHash, false, err
	}
	hash, err := buildTree(repo, merged)
	return hash, err == nil, err
}

// ThreeWayMergePullRequest creates a merge commit combining head into base.
func (s *CodeService) ThreeWayMergePullRequest(owner, repoName, base, head string, author GitAuthor) error {
	repo, err := s.openRepo(owner, repoName)
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
		return gitref.Move(repo.Storer, plumbing.NewBranchReferenceName(base), baseCommit.Hash, headCommit.Hash)
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
	sig := author.signature(now)
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
	return gitref.Move(repo.Storer, plumbing.NewBranchReferenceName(base), baseCommit.Hash, h)
}

// SquashMergePullRequest creates a single squash commit on base incorporating all head changes.
func (s *CodeService) SquashMergePullRequest(owner, repoName, base, head string, author GitAuthor) error {
	repo, err := s.openRepo(owner, repoName)
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
	sig := author.signature(now)
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
	return gitref.Move(repo.Storer, plumbing.NewBranchReferenceName(base), baseCommit.Hash, h)
}

// ApplySuggestion replaces targetLine (1-based) in filePath on branch with the replacement
// text and creates a new commit on that branch.
func (s *CodeService) ApplySuggestion(owner, repoName, branch, filePath string, targetLine int, replacement string, author GitAuthor) error {
	repo, err := s.openRepo(owner, repoName)
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
	sig := author.signature(now)
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

	return gitref.Move(repo.Storer, plumbing.NewBranchReferenceName(branch), headCommit.Hash, newHash)
}
