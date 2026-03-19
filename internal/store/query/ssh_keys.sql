-- name: CreateSSHKey :one
INSERT INTO ssh_keys (user_id, title, public_key, fingerprint)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListSSHKeysByUser :many
SELECT * FROM ssh_keys WHERE user_id = $1 ORDER BY created_at DESC;

-- name: GetSSHKeyByFingerprint :one
SELECT * FROM ssh_keys WHERE fingerprint = $1;

-- name: DeleteSSHKey :exec
DELETE FROM ssh_keys WHERE id = $1 AND user_id = $2;
