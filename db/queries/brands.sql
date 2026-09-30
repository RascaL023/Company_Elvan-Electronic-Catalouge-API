-- name: ListBrands :many
SELECT id, name, slug, created_at, updated_at
FROM brands
ORDER BY name ASC, id ASC;

-- name: GetBrandByID :one
SELECT id, name, slug, created_at, updated_at
FROM brands
WHERE id = sqlc.arg('id');

-- name: GetBrandByLegacyID :one
SELECT id, name, slug, created_at, updated_at
FROM brands
WHERE legacy_id = sqlc.arg('legacy_id');

-- name: GetBrandBySlug :one
SELECT id, name, slug, created_at, updated_at
FROM brands
WHERE slug = sqlc.arg('slug');

-- name: CreateBrand :one
INSERT INTO brands (name, slug, legacy_id)
VALUES (
    sqlc.arg('name'),
    sqlc.arg('slug'),
    sqlc.narg('legacy_id')
)
RETURNING id, name, slug, created_at, updated_at;

-- name: UpdateBrand :one
UPDATE brands
SET name       = COALESCE(sqlc.narg('name'), name),
    slug       = COALESCE(sqlc.narg('slug'), slug),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING id, name, slug, created_at, updated_at;

-- name: DeleteBrand :execrows
DELETE FROM brands WHERE id = sqlc.arg('id');
