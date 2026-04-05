package service

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// isBinaryContent reports whether data appears to be binary by scanning for null bytes
// or the DEL control character (0x7f), which is present in ELF and other binary formats.
func isBinaryContent(data []byte) bool {
	for _, b := range data {
		if b == 0 || b == 0x7f {
			return true
		}
	}
	return false
}

// IndexService indexes repository source code into the full-text search store.
type IndexService struct {
	codeSearch *store.CodeSearchStore
	code       *CodeService
}

// NewIndexService creates an IndexService backed by the given search store and code service.
func NewIndexService(codeSearch *store.CodeSearchStore, code *CodeService) *IndexService {
	return &IndexService{codeSearch: codeSearch, code: code}
}

// IndexRepo walks all blobs on the repository's default branch and upserts them
// into the code search index. Binary files are skipped.
func (s *IndexService) IndexRepo(ctx context.Context, repo *model.Repository) error {
	repoPath := s.code.repoPath(repo.OwnerName, repo.Name)
	gitRepo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("index repo open: %w", err)
	}

	commit, ref, err := resolveRef(gitRepo, repo.DefaultBranch)
	if err != nil {
		// Empty repo — nothing to index.
		return nil
	}

	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("index repo tree: %w", err)
	}

	return tree.Files().ForEach(func(f *object.File) error {
		isBin, err := f.IsBinary()
		if err != nil || isBin {
			return nil // skip binary files
		}
		contents, err := f.Contents()
		if err != nil {
			return nil // skip unreadable files
		}
		if isBinaryContent([]byte(contents)) {
			return nil
		}
		return s.codeSearch.Index(ctx, repo.ID, ref, f.Name, contents)
	})
}

// Search performs full-text search across the code index.
func (s *IndexService) Search(ctx context.Context, query string, repoID *int64, lang string, page, pageSize int) ([]model.CodeSearchResult, int, error) {
	return s.codeSearch.Search(ctx, query, repoID, lang, page, pageSize)
}

// DeleteRepo removes all indexed content for a repository.
func (s *IndexService) DeleteRepo(ctx context.Context, repoID int64) error {
	return s.codeSearch.DeleteByRepo(ctx, repoID)
}
