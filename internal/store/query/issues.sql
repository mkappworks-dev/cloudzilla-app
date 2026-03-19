-- name: GetNextIssueNumber :one
SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = $1;

-- name: CreateIssue :one
INSERT INTO issues (repo_id, number, author_id, title, body, state)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListIssues :many
SELECT i.* FROM issues i
WHERE i.repo_id = $1
ORDER BY i.number DESC;

-- name: GetIssue :one
SELECT i.* FROM issues i
WHERE i.repo_id = $1 AND i.number = $2;

-- name: UpdateIssueStateClosed :exec
UPDATE issues SET state = $1, closed_at = $2, updated_at = $3 WHERE id = $4;

-- name: UpdateIssueStateOpen :exec
UPDATE issues SET state = $1, closed_at = NULL, updated_at = $2 WHERE id = $3;
