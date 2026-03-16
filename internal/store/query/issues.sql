-- name: GetNextIssueNumber :one
SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = ?;

-- name: CreateIssue :one
INSERT INTO issues (repo_id, number, author_id, title, body, state)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListIssues :many
SELECT i.* FROM issues i
WHERE i.repo_id = ?
ORDER BY i.number DESC;

-- name: GetIssue :one
SELECT i.* FROM issues i
WHERE i.repo_id = ? AND i.number = ?;

-- name: UpdateIssueStateClosed :exec
UPDATE issues SET state = ?, closed_at = ?, updated_at = ? WHERE id = ?;

-- name: UpdateIssueStateOpen :exec
UPDATE issues SET state = ?, closed_at = NULL, updated_at = ? WHERE id = ?;
