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
)

// WikiPageMeta holds display metadata for a single wiki page.
type WikiPageMeta struct {
	Slug      string
	Title     string
	UpdatedAt time.Time
}

// orderWikiSlugs returns slugs in the order prescribed by the .order file
// content, appending any real pages not listed there alphabetically.
// Stale entries in the order content (slug not in all) are silently dropped.
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

	// Append remaining pages alphabetically.
	var rest []string
	for _, s := range all {
		if !seen[s] {
			rest = append(rest, s)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// WikiPageListMeta returns each wiki page's slug, first-heading title, and HEAD
// commit time. Returns an empty slice when the wiki has no commits yet.
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

	// Collect metadata keyed by slug.
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

	// Apply .order if present; fall back to alphabetical.
	orderContent := ""
	if f, err := commit.File(".order"); err == nil {
		orderContent, _ = f.Contents()
	}
	ordered := orderWikiSlugs(slugs, orderContent)

	pages := make([]WikiPageMeta, 0, len(ordered))
	for _, slug := range ordered {
		pages = append(pages, metaBySlug[slug])
	}
	return pages, nil
}

// firstHeading scans content for the first H1 line ("# ") and returns its
// trimmed text. Returns "" when no H1 heading is found.
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

	orderContent := ""
	if f, err := commit.File(".order"); err == nil {
		orderContent, _ = f.Contents()
	}
	return orderWikiSlugs(slugs, orderContent), nil
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

// WikiPageReorder moves slug one position up or down in the sidebar order,
// persisting the result as a .order file in the wiki repo.
func (s *CodeService) WikiPageReorder(owner, repoName, slug, direction, authorName, authorEmail string) error {
	if direction != "up" && direction != "down" {
		return fmt.Errorf("invalid direction %q: must be up or down", direction)
	}

	slugs, err := s.WikiPageList(owner, repoName)
	if err != nil {
		return err
	}

	idx := -1
	for i, s := range slugs {
		if s == slug {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("wiki page %q not found", slug)
	}

	if direction == "up" && idx == 0 {
		return nil
	}
	if direction == "down" && idx == len(slugs)-1 {
		return nil
	}

	swap := idx - 1
	if direction == "down" {
		swap = idx + 1
	}
	slugs[idx], slugs[swap] = slugs[swap], slugs[idx]

	repo, err := gogit.PlainOpen(s.wikiPath(owner, repoName))
	if err != nil {
		return fmt.Errorf("wiki open: %w", err)
	}
	content := strings.Join(slugs, "\n")
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

// WikiPageRename moves a wiki page from oldSlug to newSlug in a single commit,
// preserving the original content. Returns an error when oldSlug does not exist
// or newSlug already exists (collision).
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

	// Build the new tree: all existing entries minus oldFile, plus newFile.
	stor := repo.Storer
	now := time.Now()
	sig := object.Signature{Name: authorName, Email: authorEmail, When: now}

	entries := make([]object.TreeEntry, 0, len(existingTree.Entries))
	for _, e := range existingTree.Entries {
		if e.Name != oldFile {
			entries = append(entries, e)
		}
	}
	entries = append(entries, object.TreeEntry{
		Name: newFile,
		Mode: oldEntry.Mode,
		Hash: oldEntry.Hash,
	})
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

	if message == "" {
		message = "Rename " + oldSlug + " to " + newSlug
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

// WikiPageDelete removes a wiki page by committing a tree without the file.
func (s *CodeService) WikiPageDelete(owner, repoName, slug, authorName, authorEmail string) error {
	wPath := s.wikiPath(owner, repoName)
	repo, err := gogit.PlainOpen(wPath)
	if err != nil {
		return nil // nothing to delete
	}
	return wikiDelete(repo, slug+".md", authorName, authorEmail, "Delete "+slug)
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
