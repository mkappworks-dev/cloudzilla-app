-- name: CreateRepo :one
INSERT INTO repositories (owner_id, name, description, private, default_branch)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListRepos :many
SELECT r.* FROM repositories r
ORDER BY r.created_at DESC;

-- name: GetRepoByOwnerAndName :one
SELECT r.* FROM repositories r
JOIN users u ON u.id = r.owner_id
WHERE u.username = $1 AND r.name = $2;

-- name: GetReposByOwnerID :many
SELECT r.* FROM repositories r
WHERE r.owner_id = $1
ORDER BY r.created_at DESC;

-- name: GetPermission :one
SELECT role FROM permissions WHERE repo_id = $1 AND user_id = $2;
