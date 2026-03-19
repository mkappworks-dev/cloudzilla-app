-- name: CreateComment :one
INSERT INTO comments (repo_id, issue_id, pull_id, author_id, body)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListCommentsByIssue :many
SELECT c.* FROM comments c
WHERE c.issue_id = $1
ORDER BY c.created_at ASC;

-- name: ListCommentsByPull :many
SELECT c.* FROM comments c
WHERE c.pull_id = $1
ORDER BY c.created_at ASC;

-- name: DeleteComment :exec
DELETE FROM comments WHERE id = $1;
