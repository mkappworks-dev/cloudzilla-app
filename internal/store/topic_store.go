package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// TopicStore provides database operations for repository topic tags.
type TopicStore struct{ db *sql.DB }

// NewTopicStore creates a TopicStore backed by the given database.
func NewTopicStore(db *sql.DB) *TopicStore { return &TopicStore{db: db} }

// SetTopics replaces all topics for a repo atomically.
func (s *TopicStore) SetTopics(ctx context.Context, repoID int64, names []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("topic set begin tx: %w", err)
	}
	defer tx.Rollback()

	topicIDs := make([]int64, 0, len(names))
	for _, name := range names {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO topics (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`,
			name,
		); err != nil {
			return fmt.Errorf("topic upsert %q: %w", name, err)
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM topics WHERE name = $1`, name).Scan(&id); err != nil {
			return fmt.Errorf("topic select id %q: %w", name, err)
		}
		topicIDs = append(topicIDs, id)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM repo_topics WHERE repo_id = $1`, repoID); err != nil {
		return fmt.Errorf("repo_topics delete: %w", err)
	}

	for _, tid := range topicIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO repo_topics (repo_id, topic_id) VALUES ($1, $2)`,
			repoID, tid,
		); err != nil {
			return fmt.Errorf("repo_topics insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("topic set commit: %w", err)
	}
	return nil
}

// ListByRepo returns all topics for a repository.
func (s *TopicStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Topic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name FROM topics t
		 JOIN repo_topics rt ON t.id = rt.topic_id
		 WHERE rt.repo_id = $1 ORDER BY t.name`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("topic list by repo: %w", err)
	}
	defer rows.Close()
	return scanTopics(rows)
}

// ListReposByTopic returns public repos tagged with a given topic name, paginated.
func (s *TopicStore) ListReposByTopic(ctx context.Context, topicName string, page, pageSize int) ([]model.Repository, error) {
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description,
		        r.private, r.default_branch, r.created_at, r.updated_at
		 FROM repositories r
		 JOIN repo_topics rt ON r.id = rt.repo_id
		 JOIN topics t ON t.id = rt.topic_id
		 WHERE t.name = $1 AND r.private = false
		 ORDER BY r.updated_at DESC
		 LIMIT $2 OFFSET $3`,
		topicName, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("topic list repos: %w", err)
	}
	defer rows.Close()
	return scanRepos(rows)
}

// ListReposByTopicWithStats returns public repos with star counts, paginated and sorted.
func (s *TopicStore) ListReposByTopicWithStats(ctx context.Context, topicName string, page, pageSize int, sort string) ([]model.RepositoryWithStats, error) {
	var orderBy string
	switch sort {
	case "updated":
		orderBy = "r.updated_at DESC"
	case "name":
		orderBy = "r.name ASC"
	default:
		orderBy = "star_count DESC, r.updated_at DESC"
	}
	offset := (page - 1) * pageSize
	q := `SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private,
	             r.default_branch, r.created_at, r.updated_at, r.is_fork, r.fork_of_id,
	             COALESCE((SELECT COUNT(*) FROM stars s WHERE s.repo_id = r.id), 0) AS star_count,
	             r.fork_count
	      FROM repositories r
	      JOIN repo_topics rt ON r.id = rt.repo_id
	      JOIN topics t ON t.id = rt.topic_id
	      WHERE t.name = $1 AND r.private = false
	      ORDER BY ` + orderBy + `
	      LIMIT $2 OFFSET $3`
	rows, err := s.db.QueryContext(ctx, q, topicName, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("topic list repos with stats: %w", err)
	}
	defer rows.Close()
	return scanReposWithStats(rows)
}

// CountReposByTopic returns the total number of public repos tagged with a topic.
func (s *TopicStore) CountReposByTopic(ctx context.Context, topicName string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		 FROM repositories r
		 JOIN repo_topics rt ON r.id = rt.repo_id
		 JOIN topics t ON t.id = rt.topic_id
		 WHERE t.name = $1 AND r.private = false`,
		topicName,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("topic count repos: %w", err)
	}
	return count, nil
}

func scanTopics(rows *sql.Rows) ([]model.Topic, error) {
	var topics []model.Topic
	for rows.Next() {
		var t model.Topic
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}
