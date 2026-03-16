-- name: CreateSSHKey :one
INSERT INTO ssh_keys (user_id, title, public_key, fingerprint)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListSSHKeysByUser :many
SELECT * FROM ssh_keys WHERE user_id = ? ORDER BY created_at DESC;

-- name: GetSSHKeyByFingerprint :one
SELECT * FROM ssh_keys WHERE fingerprint = ?;

-- name: DeleteSSHKey :exec
DELETE FROM ssh_keys WHERE id = ? AND user_id = ?;
