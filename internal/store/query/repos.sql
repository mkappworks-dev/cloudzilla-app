-- name: CreateRepo :one
INSERT INTO repositories (owner_id, name, description, private, default_branch)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListRepos :many
SELECT r.* FROM repositories r
ORDER BY r.created_at DESC;

-- name: GetRepoByOwnerAndName :one
SELECT r.* FROM repositories r
JOIN users u ON u.id = r.owner_id
WHERE u.username = ? AND r.name = ?;

-- name: GetReposByOwnerID :many
SELECT r.* FROM repositories r
WHERE r.owner_id = ?
ORDER BY r.created_at DESC;

-- name: GetPermission :one
SELECT role FROM permissions WHERE repo_id = ? AND user_id = ?;
