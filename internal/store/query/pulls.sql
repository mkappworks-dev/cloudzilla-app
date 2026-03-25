-- name: GetNextPullNumber :one
SELECT COALESCE(MAX(number), 0) + 1 FROM pull_requests WHERE repo_id = $1;

-- name: CreatePull :one
INSERT INTO pull_requests (repo_id, number, author_id, title, body, state, head_branch, base_branch)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListPulls :many
SELECT p.* FROM pull_requests p
WHERE p.repo_id = $1
ORDER BY p.number DESC;

-- name: GetPull :one
SELECT p.* FROM pull_requests p
WHERE p.repo_id = $1 AND p.number = $2;

-- name: UpdatePullStateMerged :exec
UPDATE pull_requests SET state = $1, merged_at = $2, updated_at = $3 WHERE id = $4;

-- name: UpdatePullStateClosed :exec
UPDATE pull_requests SET state = $1, closed_at = $2, updated_at = $3 WHERE id = $4;

-- name: UpdatePullStateOpen :exec
UPDATE pull_requests SET state = $1, updated_at = $2 WHERE id = $3;

-- name: SetAutoMerge :exec
UPDATE pull_requests
SET auto_merge_enabled  = $2,
    auto_merge_strategy = $3,
    updated_at          = NOW()
WHERE id = $1;

-- name: ListOpen :many
SELECT * FROM pull_requests
WHERE repo_id = $1 AND state = 'open'
ORDER BY number DESC;

-- name: GetPullByID :one
SELECT * FROM pull_requests WHERE id = $1;
