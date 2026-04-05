package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type CodeSearchStore struct{ db *sql.DB }

func NewCodeSearchStore(db *sql.DB) *CodeSearchStore { return &CodeSearchStore{db: db} }

// Index upserts a file's content into the search index.
func (s *CodeSearchStore) Index(ctx context.Context, repoID int64, ref, filePath, content string) error {
	const q = `
INSERT INTO code_search_index (repo_id, ref, file_path, content)
VALUES ($1, $2, $3, $4)
ON CONFLICT (repo_id, file_path) DO UPDATE
    SET content = $4, ref = $2, indexed_at = NOW()`
	_, err := s.db.ExecContext(ctx, q, repoID, ref, filePath, content)
	if err != nil {
		return fmt.Errorf("code search index upsert: %w", err)
	}
	return nil
}

// Search performs full-text search across the code index.
// query is required; repoID and lang are optional filters.
// lang is matched as a file extension suffix (e.g. ".go", ".py").
// Returns results, total hit count, and any error.
func (s *CodeSearchStore) Search(ctx context.Context, query string, repoID *int64, lang string, page, pageSize int) ([]model.CodeSearchResult, int, error) {
	args := []interface{}{query}
	argIdx := 2

	filters := []string{"csi.tsv @@ plainto_tsquery('simple', $1)"}
	if repoID != nil {
		filters = append(filters, fmt.Sprintf("csi.repo_id = $%d", argIdx))
		args = append(args, *repoID)
		argIdx++
	}
	if lang != "" {
		// Escape LIKE special characters so user input is treated as a literal suffix.
		safeLang := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(lang)
		filters = append(filters, fmt.Sprintf(`csi.file_path LIKE $%d ESCAPE '\'`, argIdx))
		args = append(args, "%"+safeLang)
		argIdx++
	}

	where := strings.Join(filters, " AND ")

	// Count total matches
	countQ := fmt.Sprintf(`
SELECT COUNT(*)
FROM code_search_index csi
JOIN repositories r ON r.id = csi.repo_id
JOIN users u ON u.id = r.owner_id
WHERE %s
  AND r.private = FALSE`, where)

	var total int
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("code search count: %w", err)
	}

	offset := (page - 1) * pageSize
	searchArgs := append(args, pageSize, offset)

	selectQ := fmt.Sprintf(`
SELECT csi.repo_id,
       r.name        AS repo_name,
       u.username    AS owner_name,
       csi.file_path,
       ts_headline('simple', csi.content, plainto_tsquery('simple', $1),
                   'MaxWords=15, MinWords=5, StartSel=**, StopSel=**') AS snippet,
       ts_rank(csi.tsv, plainto_tsquery('simple', $1))                 AS rank
FROM code_search_index csi
JOIN repositories r ON r.id = csi.repo_id
JOIN users u ON u.id = r.owner_id
WHERE %s
  AND r.private = FALSE
ORDER BY rank DESC
LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1)

	rows, err := s.db.QueryContext(ctx, selectQ, searchArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("code search query: %w", err)
	}
	defer rows.Close()

	var results []model.CodeSearchResult
	for rows.Next() {
		var res model.CodeSearchResult
		if err := rows.Scan(&res.RepoID, &res.RepoName, &res.OwnerName, &res.FilePath, &res.Snippet, &res.Rank); err != nil {
			return nil, 0, err
		}
		results = append(results, res)
	}
	return results, total, rows.Err()
}

// DeleteByRepo removes all indexed files for a given repo (used when a repo is deleted).
func (s *CodeSearchStore) DeleteByRepo(ctx context.Context, repoID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM code_search_index WHERE repo_id = $1`, repoID)
	return err
}
