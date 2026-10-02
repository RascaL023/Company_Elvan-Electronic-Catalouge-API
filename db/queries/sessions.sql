-- name: CreateSession :one
INSERT INTO sessions (admin_id, token_hash, expires_at)
VALUES (
    sqlc.arg('admin_id'),
    sqlc.arg('token_hash'),
    sqlc.arg('expires_at')
)
RETURNING id, admin_id, token_hash, expires_at, created_at, last_seen_at;

-- name: GetSessionByTokenHash :one
SELECT id, admin_id, token_hash, expires_at, created_at, last_seen_at
FROM sessions
WHERE token_hash = sqlc.arg('token_hash');

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = now()
WHERE token_hash = sqlc.arg('token_hash');

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token_hash = sqlc.arg('token_hash');

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE expires_at < sqlc.arg('before');
