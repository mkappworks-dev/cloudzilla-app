package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	gogitdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/mkappworks/cloudzilla/internal/config"
)

// ErrEmptyRepo is returned when a repository has no commits.
var ErrEmptyRepo = errors.New("repository is empty")

type BranchInfo struct {
	Name      string
	Hash      string
	IsDefault bool
}

type TagInfo struct {
	Name string
	Hash string
}

type RefsResult struct {
	Branches []BranchInfo
	Tags     []TagInfo
}

type BreadcrumbPart struct {
	Name string
	URL  string
}

type CodeLine struct {
	Num  int
	Text string
}

type BlameLine struct {
	LineNum    int
	Text       string
	Hash       string
	Author     string
	AuthorTime time.Time
	ShowMeta   bool
}

type TreeEntry struct {
	Name  string
	IsDir bool
	URL   string
}

type TreeResult struct {
	Entries     []TreeEntry
	Ref         string
	Path        string
	Breadcrumbs []BreadcrumbPart
}

type BlobResult struct {
	Ref         string
	Path        string
	Lines       []CodeLine
	IsBinary    bool
	Breadcrumbs []BreadcrumbPart
	BlameURL    string
}

type BlameResult struct {
	Ref         string
	Path        string
	Lines       []BlameLine
	Breadcrumbs []BreadcrumbPart
	BlobURL     string
}

type CodeService struct {
	cfg config.GitConfig
}

func NewCodeService(cfg config.GitConfig) *CodeService {
	return &CodeService{cfg: cfg}
}

func (s *CodeService) repoPath(owner, repoName string) string {
	return filepath.Join(s.cfg.ReposRoot, owner, repoName+".git")
}

// ResolveRef resolves a ref string to a commit. Priority: branch → tag → SHA → HEAD.
// Returns ErrEmptyRepo if the repo has no commits.
func (s *CodeService) ResolveRef(owner, repoName, ref string) (*object.Commit, string, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, "", err
	}
	return resolveRef(repo, ref)
}

func resolveRef(repo *gogit.Repository, ref string) (*object.Commit, string, error) {
	if ref != "" {
		// Try branch
		if branchRef, err := repo.Reference(plumbing.NewBranchReferenceName(ref), true); err == nil {
			if commit, err := repo.CommitObject(branchRef.Hash()); err == nil {
				return commit, ref, nil
			}
		}
		// Try tag
		if tagRef, err := repo.Reference(plumbing.NewTagReferenceName(ref), true); err == nil {
			if commit, err := repo.CommitObject(tagRef.Hash()); err == nil {
				return commit, ref, nil
			}
		}
		// Try raw SHA
		hash := plumbing.NewHash(ref)
		if commit, err := repo.CommitObject(hash); err == nil {
			return commit, ref[:7], nil
		}
		return nil, "", errors.New("ref not found: " + ref)
	}

	// Fall back to HEAD
	head, err := repo.Head()
	if err != nil {
		return nil, "", ErrEmptyRepo
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, "", ErrEmptyRepo
	}
	displayRef := head.Name().Short()
	return commit, displayRef, nil
}

func buildBreadcrumbs(owner, repoName, ref, path string, isBlob bool) []BreadcrumbPart {
	parts := []BreadcrumbPart{
		{Name: owner, URL: "/" + owner},
		{Name: repoName, URL: "/" + owner + "/" + repoName},
	}
	if path == "" {
		return parts
	}
	segments := strings.Split(path, "/")
	accumulated := ""
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		if accumulated == "" {
			accumulated = seg
		} else {
			accumulated += "/" + seg
		}
		isLast := i == len(segments)-1
		var url string
		if isLast && isBlob {
			url = "/" + owner + "/" + repoName + "/blob/" + ref + "/" + accumulated
		} else {
			url = "/" + owner + "/" + repoName + "/tree/" + ref + "/" + accumulated
		}
		parts = append(parts, BreadcrumbPart{Name: seg, URL: url})
	}
	return parts
}

// GetTree returns tree entries for the given path (empty = root).
func (s *CodeService) GetTree(owner, repoName, ref, path string) (*TreeResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	rootTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	var tree *object.Tree
	if path == "" {
		tree = rootTree
	} else {
		tree, err = rootTree.Tree(path)
		if err != nil {
			return nil, err
		}
	}

	var dirs, files []TreeEntry
	for _, entry := range tree.Entries {
		isDir := entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule
		var entryPath string
		if path == "" {
			entryPath = entry.Name
		} else {
			entryPath = path + "/" + entry.Name
		}
		var url string
		if isDir {
			url = "/" + owner + "/" + repoName + "/tree/" + displayRef + "/" + entryPath
		} else {
			url = "/" + owner + "/" + repoName + "/blob/" + displayRef + "/" + entryPath
		}
		e := TreeEntry{Name: entry.Name, IsDir: isDir, URL: url}
		if isDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	entries := append(dirs, files...)

	return &TreeResult{
		Entries:     entries,
		Ref:         displayRef,
		Path:        path,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, displayRef, path, false),
	}, nil
}

// GetBlob returns file content. Sets IsBinary=true for binary files.
func (s *CodeService) GetBlob(owner, repoName, ref, path string) (*BlobResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	file, err := commit.File(path)
	if err != nil {
		return nil, err
	}

	isBinary, err := file.IsBinary()
	if err != nil {
		return nil, err
	}

	result := &BlobResult{
		Ref:         displayRef,
		Path:        path,
		IsBinary:    isBinary,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, displayRef, path, true),
		BlameURL:    "/" + owner + "/" + repoName + "/blame/" + displayRef + "/" + path,
	}

	if !isBinary {
		contents, err := file.Contents()
		if err != nil {
			return nil, err
		}
		rawLines := strings.Split(contents, "\n")
		lines := make([]CodeLine, len(rawLines))
		for i, text := range rawLines {
			lines[i] = CodeLine{Num: i + 1, Text: text}
		}
		result.Lines = lines
	}

	return result, nil
}

type CommitSummary struct {
	Hash       string
	FullHash   string
	Message    string
	Author     string
	AuthorTime time.Time
}

type CommitLog struct {
	Commits  []CommitSummary
	Ref      string
	Page     int
	PrevPage int
	NextPage int
	HasMore  bool
}

type DiffLine struct {
	Type    string // "add", "del", "ctx"
	Content string
	OldNum  int
	NewNum  int
}

type DiffHunk struct {
	Header string
	Lines  []DiffLine
}

type FileDiff struct {
	OldPath  string
	NewPath  string
	IsBinary bool
	IsNew    bool
	IsDelete bool
	Added    int
	Deleted  int
	Hunks    []DiffHunk
}

type CommitDetail struct {
	Hash         string
	FullHash     string
	Message      string
	Body         string
	Author       string
	AuthorEmail  string
	AuthorTime   time.Time
	ParentHashes []string
	Files        []FileDiff
	TotalAdded   int
	TotalDeleted int
}

// GetCommits returns a paginated commit log for the given ref.
func (s *CodeService) GetCommits(owner, repoName, ref string, page, pageSize int) (*CommitLog, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	iter, err := repo.Log(&gogit.LogOptions{From: commit.Hash, Order: gogit.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	skip := (page - 1) * pageSize
	var commits []CommitSummary
	count := 0

	err = iter.ForEach(func(c *object.Commit) error {
		if count < skip {
			count++
			return nil
		}
		if len(commits) > pageSize {
			return storer.ErrStop
		}
		hash := c.Hash.String()
		shortHash := hash
		if len(hash) > 7 {
			shortHash = hash[:7]
		}
		msg := strings.TrimSpace(c.Message)
		firstLine := msg
		if idx := strings.Index(msg, "\n"); idx >= 0 {
			firstLine = strings.TrimSpace(msg[:idx])
		}
		commits = append(commits, CommitSummary{
			Hash:       shortHash,
			FullHash:   hash,
			Message:    firstLine,
			Author:     c.Author.Name,
			AuthorTime: c.Author.When,
		})
		count++
		return nil
	})
	if err != nil {
		return nil, err
	}

	hasMore := len(commits) > pageSize
	if hasMore {
		commits = commits[:pageSize]
	}

	prevPage := 0
	if page > 1 {
		prevPage = page - 1
	}
	nextPage := 0
	if hasMore {
		nextPage = page + 1
	}

	return &CommitLog{
		Commits:  commits,
		Ref:      displayRef,
		Page:     page,
		PrevPage: prevPage,
		NextPage: nextPage,
		HasMore:  hasMore,
	}, nil
}

// GetCommit returns a single commit with full diff.
func (s *CodeService) GetCommit(owner, repoName, sha string) (*CommitDetail, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}

	commit, err := repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return nil, err
	}

	shortHash := sha
	if len(sha) > 7 {
		shortHash = sha[:7]
	}

	msg := strings.TrimSpace(commit.Message)
	firstLine := msg
	body := ""
	if idx := strings.Index(msg, "\n"); idx >= 0 {
		firstLine = strings.TrimSpace(msg[:idx])
		body = strings.TrimSpace(msg[idx+1:])
	}

	var parentHashes []string
	for _, ph := range commit.ParentHashes {
		h := ph.String()
		if len(h) > 7 {
			h = h[:7]
		}
		parentHashes = append(parentHashes, h)
	}

	commitTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	var patch *object.Patch
	if commit.NumParents() == 0 {
		changes, err := object.DiffTree(nil, commitTree)
		if err != nil {
			return nil, err
		}
		patch, err = changes.Patch()
		if err != nil {
			return nil, err
		}
	} else {
		parent, err := commit.Parent(0)
		if err != nil {
			return nil, err
		}
		patch, err = parent.Patch(commit)
		if err != nil {
			return nil, err
		}
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

	return &CommitDetail{
		Hash:         shortHash,
		FullHash:     commit.Hash.String(),
		Message:      firstLine,
		Body:         body,
		Author:       commit.Author.Name,
		AuthorEmail:  commit.Author.Email,
		AuthorTime:   commit.Author.When,
		ParentHashes: parentHashes,
		Files:        files,
		TotalAdded:   totalAdded,
		TotalDeleted: totalDeleted,
	}, nil
}

func buildHunks(chunks []gogitdiff.Chunk) []DiffHunk {
	const ctx = 3

	type lineInfo struct {
		op      gogitdiff.Operation
		content string
		oldNum  int
		newNum  int
	}
	var lines []lineInfo
	oldN, newN := 1, 1
	for _, chunk := range chunks {
		content := strings.TrimSuffix(chunk.Content(), "\n")
		parts := strings.Split(content, "\n")
		for _, part := range parts {
			switch chunk.Type() {
			case gogitdiff.Equal:
				lines = append(lines, lineInfo{gogitdiff.Equal, part, oldN, newN})
				oldN++
				newN++
			case gogitdiff.Add:
				lines = append(lines, lineInfo{gogitdiff.Add, part, 0, newN})
				newN++
			case gogitdiff.Delete:
				lines = append(lines, lineInfo{gogitdiff.Delete, part, oldN, 0})
				oldN++
			}
		}
	}

	include := make([]bool, len(lines))
	for i, l := range lines {
		if l.op != gogitdiff.Equal {
			lo, hi := max(0, i-ctx), min(len(lines)-1, i+ctx)
			for j := lo; j <= hi; j++ {
				include[j] = true
			}
		}
	}

	var hunks []DiffHunk
	for i := 0; i < len(lines); {
		if !include[i] {
			i++
			continue
		}
		var hunkLines []DiffLine
		for i < len(lines) && include[i] {
			l := lines[i]
			opMap := map[gogitdiff.Operation]string{
				gogitdiff.Add:    "add",
				gogitdiff.Delete: "del",
				gogitdiff.Equal:  "ctx",
			}
			hunkLines = append(hunkLines, DiffLine{opMap[l.op], l.content, l.oldNum, l.newNum})
			i++
		}
		oldStart, newStart, oldCount, newCount := 0, 0, 0, 0
		for _, hl := range hunkLines {
			if hl.Type != "add" {
				oldCount++
				if oldStart == 0 && hl.OldNum > 0 {
					oldStart = hl.OldNum
				}
			}
			if hl.Type != "del" {
				newCount++
				if newStart == 0 && hl.NewNum > 0 {
					newStart = hl.NewNum
				}
			}
		}
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
		hunks = append(hunks, DiffHunk{Header: header, Lines: hunkLines})
	}
	return hunks
}

// ListRefs returns all branches and tags for a repository.
func (s *CodeService) ListRefs(owner, repoName, defaultBranch string) (*RefsResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}

	var branches []BranchInfo
	branchIter, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	_ = branchIter.ForEach(func(ref *plumbing.Reference) error {
		hash := ref.Hash().String()
		if len(hash) > 7 {
			hash = hash[:7]
		}
		branches = append(branches, BranchInfo{
			Name:      ref.Name().Short(),
			Hash:      hash,
			IsDefault: ref.Name().Short() == defaultBranch,
		})
		return nil
	})
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })

	var tags []TagInfo
	tagIter, err := repo.Tags()
	if err != nil {
		return nil, err
	}
	_ = tagIter.ForEach(func(ref *plumbing.Reference) error {
		hash := ref.Hash().String()
		if len(hash) > 7 {
			hash = hash[:7]
		}
		tags = append(tags, TagInfo{
			Name: ref.Name().Short(),
			Hash: hash,
		})
		return nil
	})
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })

	return &RefsResult{Branches: branches, Tags: tags}, nil
}

// CreateBranch creates a new branch pointing to the resolved fromRef commit.
func (s *CodeService) CreateBranch(owner, repoName, name, fromRef string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	if _, err := repo.Reference(plumbing.NewBranchReferenceName(name), true); err == nil {
		return errors.New("branch already exists: " + name)
	}
	commit, _, err := resolveRef(repo, fromRef)
	if err != nil {
		return fmt.Errorf("from ref not found: %w", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), commit.Hash)
	return repo.Storer.SetReference(ref)
}

// DeleteBranch removes the named branch reference.
func (s *CodeService) DeleteBranch(owner, repoName, name string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	return repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(name))
}

// CreateTag creates a new lightweight tag pointing to the resolved fromRef commit.
func (s *CodeService) CreateTag(owner, repoName, name, fromRef string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	if _, err := repo.Reference(plumbing.NewTagReferenceName(name), true); err == nil {
		return errors.New("tag already exists: " + name)
	}
	commit, _, err := resolveRef(repo, fromRef)
	if err != nil {
		return fmt.Errorf("from ref not found: %w", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewTagReferenceName(name), commit.Hash)
	return repo.Storer.SetReference(ref)
}

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
		return nil, errors.New("no common ancestor")
	}
	return base, nil
}

// buildTree recursively encodes a flat file map into git tree objects and returns the root tree hash.
func buildTree(repo *gogit.Repository, files map[string]mergeFile) (plumbing.Hash, error) {
	// Group entries by top-level directory segment.
	type dirEntry struct {
		name    string
		subpath string // remaining path after the top-level segment
		mf      mergeFile
		isBlob  bool
	}
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

// DeleteTag removes the named tag reference.
func (s *CodeService) DeleteTag(owner, repoName, name string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	return repo.Storer.RemoveReference(plumbing.NewTagReferenceName(name))
}

// GetBlame returns per-line blame information.
func (s *CodeService) GetBlame(owner, repoName, ref, path string) (*BlameResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	blameResult, err := gogit.Blame(commit, path)
	if err != nil {
		return nil, err
	}

	lines := make([]BlameLine, len(blameResult.Lines))
	for i, l := range blameResult.Lines {
		hash := l.Hash.String()
		if len(hash) > 7 {
			hash = hash[:7]
		}
		showMeta := i == 0 || blameResult.Lines[i].Hash != blameResult.Lines[i-1].Hash
		lines[i] = BlameLine{
			LineNum:    i + 1,
			Text:       l.Text,
			Hash:       hash,
			Author:     l.Author,
			AuthorTime: l.Date,
			ShowMeta:   showMeta,
		}
	}

	return &BlameResult{
		Ref:         displayRef,
		Path:        path,
		Lines:       lines,
		Breadcrumbs: buildBreadcrumbs(owner, repoName, displayRef, path, true),
		BlobURL:     "/" + owner + "/" + repoName + "/blob/" + displayRef + "/" + path,
	}, nil
}
