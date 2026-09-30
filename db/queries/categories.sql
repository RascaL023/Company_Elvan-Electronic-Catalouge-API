-- name: ListCategories :many
SELECT id, name, slug, description, created_at, updated_at
FROM categories
ORDER BY name ASC, id ASC;

-- name: GetCategoryByID :one
SELECT id, name, slug, description, created_at, updated_at
FROM categories
WHERE id = sqlc.arg('id');

-- name: GetCategoryByLegacyID :one
SELECT id, name, slug, description, created_at, updated_at
FROM categories
WHERE legacy_id = sqlc.arg('legacy_id');

-- name: CreateCategory :one
INSERT INTO categories (name, slug, description, legacy_id)
VALUES (
    sqlc.arg('name'),
    sqlc.arg('slug'),
    sqlc.arg('description'),
    sqlc.narg('legacy_id')
)
RETURNING id, name, slug, description, created_at, updated_at;

-- name: UpdateCategory :one
UPDATE categories
SET name        = COALESCE(sqlc.narg('name'), name),
    slug        = COALESCE(sqlc.narg('slug'), slug),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at  = now()
WHERE id = sqlc.arg('id')
RETURNING id, name, slug, description, created_at, updated_at;

-- name: DeleteCategory :execrows
DELETE FROM categories WHERE id = sqlc.arg('id');
