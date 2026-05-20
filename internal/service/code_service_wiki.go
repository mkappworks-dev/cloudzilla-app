package service

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type WikiPageMeta struct {
	Slug      string
	Title     string
	UpdatedAt time.Time
}

func orderWikiSlugs(all []string, orderContent string) []string {
	set := make(map[string]bool, len(all))
	for _, s := range all {
		set[s] = true
	}

	var ordered []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(orderContent, "\n") {
		slug := strings.TrimSpace(line)
		if slug == "" || !set[slug] || seen[slug] {
			continue
		}
		ordered = append(ordered, slug)
		seen[slug] = true
	}

	var rest []string
	for _, s := range all {
		if !seen[s] {
			rest = append(rest, s)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// readWikiOrder returns the raw .order content for a commit, or "" when the
// blob does not exist. A blob that exists but cannot be read is logged at
// warn level so the user-visible "alphabetical fallback" is never silent.
func readWikiOrder(commit *object.Commit, owner, repoName string) string {
	f, err := commit.File(".order")
	if err != nil {
		return ""
	}
	content, cerr := f.Contents()
	if cerr != nil {
		slog.Warn("wiki: .order read failed; falling back to alphabetical",
			"owner", owner, "repo", repoName, "error", cerr)
		return ""
	}
	return content
}

// rewriteWikiOrder returns updated .order content with slug substitution
// (oldSlug→newSlug when newSlug != "") or removal (newSlug == ""). Returns
// empty + false when the input had no usable lines, signalling callers to
// skip writing a .order blob entirely.
func rewriteWikiOrder(orderContent, oldSlug, newSlug string) (string, bool) {
	if orderContent == "" {
		return "", false
	}
	var out []string
	for _, line := range strings.Split(orderContent, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if s == oldSlug {
			if newSlug == "" {
				continue
			}
			out = append(out, newSlug)
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "\n"), true
}

// WikiPageListMeta returns each wiki page's slug, first-heading title, and HEAD
// commit time. Empty slice when the wiki has no commits yet.
func (s *CodeService) WikiPageListMeta(owner, repoName string) ([]WikiPageMeta, error) {
	repo, err := gogit.PlainOpen(s.wikiPath(owner, repoName))
	if err != nil {
		if errors.Is(err, gogit.ErrRepositoryNotExists) {
			return []WikiPageMeta{}, nil
		}
		return nil, fmt.Errorf("wiki open: %w", err)
	}
	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return []WikiPageMeta{}, nil
		}
		return nil, fmt.Errorf("wiki head: %w", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	updatedAt := commit.Author.When

	metaBySlug := make(map[string]WikiPageMeta)
	var slugs []string
	for _, entry := range tree.Entries {
		if !strings.HasSuffix(entry.Name, ".md") {
			continue
		}
		slug := strings.TrimSuffix(entry.Name, ".md")
		title := slug
		f, err := commit.File(entry.Name)
		if err == nil {
			contents, err := f.Contents()
			if err == nil {
				if h := firstHeading(contents); h != "" {
					title = h
				}
			}
		}
		metaBySlug[slug] = WikiPageMeta{Slug: slug, Title: title, UpdatedAt: updatedAt}
		slugs = append(slugs, slug)
	}

	ordered := orderWikiSlugs(slugs, readWikiOrder(commit, owner, repoName))

	pages := make([]WikiPageMeta, 0, len(ordered))
	for _, slug := range ordered {
		pages = append(pages, metaBySlug[slug])
	}
	return pages, nil
}

func firstHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(trimmed[2:])
		}
	}
	return ""
}

// WikiPageList opens the wiki bare repo (if it exists) and returns page slugs
// (filenames without the .md extension) from the HEAD tree root.
// Returns an empty slice when the wiki has no commits yet.
func (s *CodeService) WikiPageList(owner, repoName string) ([]string, error) {
	repo, err := gogit.PlainOpen(s.wikiPath(owner, repoName))
	if err != nil {
		// Wiki repo does not exist yet — not an error.
		return []string{}, nil
	}
	head, err := repo.Head()
	if err != nil {
		// Empty repo — no commits yet.
		return []string{}, nil
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var slugs []string
	for _, entry := range tree.Entries {
		if strings.HasSuffix(entry.Name, ".md") {
			slugs = append(slugs, strings.TrimSuffix(entry.Name, ".md"))
		}
	}

	return orderWikiSlugs(slugs, readWikiOrder(commit, owner, repoName)), nil
}

// WikiPageGet returns the raw Markdown content of a single wiki page identified
// by its slug (filename without .md).
// Returns ("", false, nil) when the page does not exist and ("", true, nil) when
// the page exists but has no content. Callers must use found to distinguish the
// two cases.
func (s *CodeService) WikiPageGet(owner, repoName, slug string) (content string, found bool, err error) {
	repo, err := gogit.PlainOpen(s.wikiPath(owner, repoName))
	if err != nil {
		return "", false, nil
	}
	head, err := repo.Head()
	if err != nil {
		return "", false, nil
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return "", false, err
	}
	f, err := commit.File(slug + ".md")
	if err != nil {
		// Page not found in tree — not an error for the caller.
		return "", false, nil
	}
	c, err := f.Contents()
	return c, true, err
}

// WikiPageSetOrder persists orderedSlugs as the sidebar order by committing a
// new .order file to the wiki bare repo. Returns an error when the wiki repo
// does not yet exist.
func (s *CodeService) WikiPageSetOrder(owner, repoName string, orderedSlugs []string, authorName, authorEmail string) error {
	repo, err := gogit.PlainOpen(s.wikiPath(owner, repoName))
	if err != nil {
		return fmt.Errorf("wiki open: %w", err)
	}
	content := strings.Join(orderedSlugs, "\n")
	return wikiCommit(repo, ".order", []byte(content), authorName, authorEmail, "Reorder wiki pages")
}

// WikiPageSave creates or updates a wiki page in the bare repo, creating the
// repo itself (with an initial empty commit) if it does not yet exist.
// authorName and authorEmail are used for the git commit signature.
// message is the commit message; if empty, a default is used.
func (s *CodeService) WikiPageSave(owner, repoName, slug, content, authorName, authorEmail, message string) error {
	if message == "" {
		message = "Update " + slug
	}
	wPath := s.wikiPath(owner, repoName)

	// Open or initialise the bare wiki repo.
	repo, err := gogit.PlainOpen(wPath)
	if err != nil {
		repo, err = gogit.PlainInit(wPath, true)
		if err != nil {
			return fmt.Errorf("wiki init: %w", err)
		}
	}

	// Build the new tree by reading the current HEAD tree (if any) and
	// inserting / replacing the target file, then writing a new commit.
	return wikiCommit(repo, slug+".md", []byte(content), authorName, authorEmail, message)
}

// WikiPageRename moves a page in a single commit and rewrites .order so the
// user-defined sidebar position is preserved across the rename.
func (s *CodeService) WikiPageRename(owner, repoName, oldSlug, newSlug, authorName, authorEmail, message string) error {
	wPath := s.wikiPath(owner, repoName)
	repo, err := gogit.PlainOpen(wPath)
	if err != nil {
		return fmt.Errorf("wiki open: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("wiki head: %w", err)
	}
	parentCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return err
	}
	existingTree, err := parentCommit.Tree()
	if err != nil {
		return err
	}

	oldFile := oldSlug + ".md"
	newFile := newSlug + ".md"

	var oldEntry *object.TreeEntry
	for i := range existingTree.Entries {
		switch existingTree.Entries[i].Name {
		case oldFile:
			e := existingTree.Entries[i]
			oldEntry = &e
		case newFile:
			return fmt.Errorf("a page named %q already exists", newSlug)
		}
	}
	if oldEntry == nil {
		return fmt.Errorf("page %q not found", oldSlug)
	}

	if message == "" {
		message = "Rename " + oldSlug + " to " + newSlug
	}

	mutate := func(entries []object.TreeEntry) ([]object.TreeEntry, []blobWrite, error) {
		out := make([]object.TreeEntry, 0, len(entries))
		for _, e := range entries {
			if e.Name == oldFile {
				continue
			}
			out = append(out, e)
		}
		out = append(out, object.TreeEntry{Name: newFile, Mode: oldEntry.Mode, Hash: oldEntry.Hash})

		var blobs []blobWrite
		if orderContent := readWikiOrder(parentCommit, owner, repoName); orderContent != "" {
			if rewritten, ok := rewriteWikiOrder(orderContent, oldSlug, newSlug); ok {
				blobs = append(blobs, blobWrite{Name: ".order", Content: []byte(rewritten)})
			}
		}
		return out, blobs, nil
	}
	return wikiMutateTree(repo, parentCommit, authorName, authorEmail, message, mutate)
}

// WikiPageDelete removes a wiki page and strips the slug from .order so the
// sidebar order does not silently drift toward a dead entry.
func (s *CodeService) WikiPageDelete(owner, repoName, slug, authorName, authorEmail string) error {
	wPath := s.wikiPath(owner, repoName)
	repo, err := gogit.PlainOpen(wPath)
	if err != nil {
		return nil
	}
	head, err := repo.Head()
	if err != nil {
		return nil
	}
	parentCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return err
	}

	filename := slug + ".md"
	mutate := func(entries []object.TreeEntry) ([]object.TreeEntry, []blobWrite, error) {
		out := make([]object.TreeEntry, 0, len(entries))
		found := false
		for _, e := range entries {
			if e.Name == filename {
				found = true
				continue
			}
			out = append(out, e)
		}
		if !found {
			return nil, nil, errWikiNoChange
		}

		var blobs []blobWrite
		if orderContent := readWikiOrder(parentCommit, owner, repoName); orderContent != "" {
			if rewritten, ok := rewriteWikiOrder(orderContent, slug, ""); ok {
				blobs = append(blobs, blobWrite{Name: ".order", Content: []byte(rewritten)})
			} else {
				// All entries stripped — replace .order with an empty blob so
				// any prior stale slug references are also flushed.
				blobs = append(blobs, blobWrite{Name: ".order", Content: []byte("")})
			}
		}
		return out, blobs, nil
	}
	if err := wikiMutateTree(repo, parentCommit, authorName, authorEmail, "Delete "+slug, mutate); err != nil {
		if errors.Is(err, errWikiNoChange) {
			return nil
		}
		return err
	}
	return nil
}

// errWikiNoChange signals that a mutate callback observed nothing to do.
// wikiMutateTree turns this into a no-op rather than a vacuous commit.
var errWikiNoChange = errors.New("wiki: no change")

type blobWrite struct {
	Name    string
	Content []byte
}

// wikiMutateTree commits the result of applying mutate() to the parent commit's
// tree entries. The callback returns the new entry list (with .md changes
// applied) plus any additional blobs to write into the new tree by name —
// used here to co-update .order on rename/delete.
func wikiMutateTree(
	repo *gogit.Repository,
	parentCommit *object.Commit,
	authorName, authorEmail, message string,
	mutate func([]object.TreeEntry) ([]object.TreeEntry, []blobWrite, error),
) error {
	parentTree, err := parentCommit.Tree()
	if err != nil {
		return err
	}
	entries, blobs, err := mutate(parentTree.Entries)
	if err != nil {
		return err
	}

	stor := repo.Storer
	for _, b := range blobs {
		blobObj := stor.NewEncodedObject()
		blobObj.SetType(plumbing.BlobObject)
		blobObj.SetSize(int64(len(b.Content)))
		bw, err := blobObj.Writer()
		if err != nil {
			return err
		}
		if _, err := bw.Write(b.Content); err != nil {
			return err
		}
		if err := bw.Close(); err != nil {
			return err
		}
		hash, err := stor.SetEncodedObject(blobObj)
		if err != nil {
			return err
		}
		replaced := false
		for i, e := range entries {
			if e.Name == b.Name {
				entries[i].Hash = hash
				entries[i].Mode = filemode.Regular
				replaced = true
				break
			}
		}
		if !replaced {
			entries = append(entries, object.TreeEntry{Name: b.Name, Mode: filemode.Regular, Hash: hash})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	treeObj := stor.NewEncodedObject()
	tree := object.Tree{Entries: entries}
	if err := tree.Encode(treeObj); err != nil {
		return err
	}
	treeHash, err := stor.SetEncodedObject(treeObj)
	if err != nil {
		return err
	}

	now := time.Now()
	sig := object.Signature{Name: authorName, Email: authorEmail, When: now}
	commitObj := stor.NewEncodedObject()
	commit := object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      message,
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{parentCommit.Hash},
	}
	if err := commit.Encode(commitObj); err != nil {
		return err
	}
	commitHash, err := stor.SetEncodedObject(commitObj)
	if err != nil {
		return err
	}

	headRef, err := repo.Head()
	if err != nil {
		return err
	}
	return stor.SetReference(plumbing.NewHashReference(headRef.Name(), commitHash))
}

// wikiCommit writes filename/content into the bare repo as a new commit on
// the default (main) branch, preserving all other files from HEAD.
func wikiCommit(repo *gogit.Repository, filename string, content []byte, authorName, authorEmail, message string) error {
	now := time.Now()
	sig := object.Signature{Name: authorName, Email: authorEmail, When: now}

	storer := repo.Storer

	// Build blob object for the new file content.
	blobObj := storer.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	blobObj.SetSize(int64(len(content)))
	w, err := blobObj.Writer()
	if err != nil {
		return err
	}
	if _, err = w.Write(content); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	blobHash, err := storer.SetEncodedObject(blobObj)
	if err != nil {
		return err
	}

	// Load existing tree entries from HEAD (if any commits exist).
	entries := []object.TreeEntry{}
	var parentHashes []plumbing.Hash
	head, headErr := repo.Head()
	if headErr == nil {
		parentCommit, err := repo.CommitObject(head.Hash())
		if err != nil {
			return err
		}
		parentHashes = []plumbing.Hash{parentCommit.Hash}
		existingTree, err := parentCommit.Tree()
		if err != nil {
			return err
		}
		for _, e := range existingTree.Entries {
			if e.Name != filename {
				entries = append(entries, e)
			}
		}
	}

	// Append / replace the target file entry.
	entries = append(entries, object.TreeEntry{
		Name: filename,
		Mode: filemode.Regular,
		Hash: blobHash,
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	// Write the tree object.
	treeObj := storer.NewEncodedObject()
	tree := object.Tree{Entries: entries}
	if err := tree.Encode(treeObj); err != nil {
		return err
	}
	treeHash, err := storer.SetEncodedObject(treeObj)
	if err != nil {
		return err
	}

	// Write the commit object.
	commitObj := storer.NewEncodedObject()
	commit := object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      message,
		TreeHash:     treeHash,
		ParentHashes: parentHashes,
	}
	if err := commit.Encode(commitObj); err != nil {
		return err
	}
	commitHash, err := storer.SetEncodedObject(commitObj)
	if err != nil {
		return err
	}

	// Advance HEAD / main ref.
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), commitHash)
	if err := storer.SetReference(ref); err != nil {
		return err
	}

	// Ensure HEAD is a symbolic ref pointing at main.
	mainBranch := plumbing.NewBranchReferenceName("main")
	headRef, headErr := storer.Reference(plumbing.HEAD)
	if headErr != nil || headRef.Type() == plumbing.HashReference || headRef.Target() != mainBranch {
		symRef := plumbing.NewSymbolicReference(plumbing.HEAD, mainBranch)
		if err := storer.SetReference(symRef); err != nil {
			return fmt.Errorf("set symbolic HEAD: %w", err)
		}
	}
	return nil
}

// wikiDelete commits a tree with the named file removed.
func wikiDelete(repo *gogit.Repository, filename, authorName, authorEmail, message string) error {
	now := time.Now()
	sig := object.Signature{Name: authorName, Email: authorEmail, When: now}
	stor := repo.Storer

	head, err := repo.Head()
	if err != nil {
		return nil // empty repo, nothing to delete
	}
	parentCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return err
	}
	existingTree, err := parentCommit.Tree()
	if err != nil {
		return err
	}

	entries := []object.TreeEntry{}
	found := false
	for _, e := range existingTree.Entries {
		if e.Name == filename {
			found = true
		} else {
			entries = append(entries, e)
		}
	}
	if !found {
		return nil // file not present; skip vacuous commit
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	treeObj := stor.NewEncodedObject()
	tree := object.Tree{Entries: entries}
	if err := tree.Encode(treeObj); err != nil {
		return err
	}
	treeHash, err := stor.SetEncodedObject(treeObj)
	if err != nil {
		return err
	}

	commitObj := stor.NewEncodedObject()
	commit := object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      message,
		TreeHash:     treeHash,
		ParentHashes: []plumbing.Hash{parentCommit.Hash},
	}
	if err := commit.Encode(commitObj); err != nil {
		return err
	}
	commitHash, err := stor.SetEncodedObject(commitObj)
	if err != nil {
		return err
	}

	ref := plumbing.NewHashReference(head.Name(), commitHash)
	return stor.SetReference(ref)
}
