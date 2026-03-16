-- name: GetNextPullNumber :one
SELECT COALESCE(MAX(number), 0) + 1 FROM pull_requests WHERE repo_id = ?;

-- name: CreatePull :one
INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListPulls :many
SELECT p.* FROM pull_requests p
WHERE p.repo_id = ?
ORDER BY p.number DESC;

-- name: GetPull :one
SELECT p.* FROM pull_requests p
WHERE p.repo_id = ? AND p.number = ?;

-- name: UpdatePullStateMerged :exec
UPDATE pull_requests SET state = ?, merged_at = ?, updated_at = ? WHERE id = ?;

-- name: UpdatePullStateClosed :exec
UPDATE pull_requests SET state = ?, closed_at = ?, updated_at = ? WHERE id = ?;

-- name: UpdatePullStateOpen :exec
UPDATE pull_requests SET state = ?, updated_at = ? WHERE id = ?;
