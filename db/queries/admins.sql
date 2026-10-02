-- name: GetAdminByID :one
SELECT id, email, password_hash, created_at
FROM admins
WHERE id = sqlc.arg('id');

-- name: GetAdminByEmail :one
SELECT id, email, password_hash, created_at
FROM admins
WHERE email = sqlc.arg('email');

-- name: CreateAdmin :one
INSERT INTO admins (email, password_hash)
VALUES (sqlc.arg('email'), sqlc.arg('password_hash'))
RETURNING id, email, password_hash, created_at;

-- name: UpdateAdminPassword :exec
UPDATE admins
SET password_hash = sqlc.arg('password_hash')
WHERE id = sqlc.arg('id');
