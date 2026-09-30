-- name: ListAllProducts :many
-- Proyeksi ringan seluruh katalog (dipakai GET /catalog). Thumbnail = gambar
-- posisi 0. `include_inactive` hanya dihormati untuk admin terautentikasi.
SELECT
    p.id,
    p.name,
    p.slug,
    p.price,
    p.rating_rate,
    p.rating_count,
    p.is_active,
    p.created_at,
    c.slug AS category,
    COALESCE(b.slug, '') AS brand,
    COALESCE((
        SELECT pi.key
        FROM product_images pi
        WHERE pi.product_id = p.id
        ORDER BY pi.position
        LIMIT 1
    ), '')::text AS thumbnail
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE (sqlc.arg('include_inactive')::boolean OR p.is_active)
ORDER BY p.created_at DESC, p.id DESC;

-- name: GetProductByID :one
SELECT
    p.id, p.name, p.slug, p.price, p.description,
    p.rating_rate, p.rating_count, p.is_active,
    p.created_at, p.updated_at,
    c.slug AS category,
    COALESCE(b.slug, '') AS brand
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.id = sqlc.arg('id');

-- name: GetProductByLegacyID :one
-- Dipakai untuk menjaga URL lama (/product/{firestoreId}) setelah cutover.
SELECT
    p.id, p.name, p.slug, p.price, p.description,
    p.rating_rate, p.rating_count, p.is_active,
    p.created_at, p.updated_at,
    c.slug AS category,
    COALESCE(b.slug, '') AS brand
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE p.legacy_id = sqlc.arg('legacy_id');

-- name: CreateProduct :one
-- category/brand diberikan sebagai slug; subquery me-resolve ke foreign key.
-- brand kosong -> NULL.
INSERT INTO products (
    name, slug, price, description,
    category_id, brand_id,
    rating_rate, rating_count, is_active
)
VALUES (
    sqlc.arg('name'),
    sqlc.arg('slug'),
    sqlc.arg('price'),
    sqlc.arg('description'),
    (SELECT id FROM categories WHERE slug = sqlc.arg('category')::text),
    (SELECT id FROM brands WHERE slug = NULLIF(sqlc.arg('brand')::text, '')),
    sqlc.arg('rating_rate'),
    sqlc.arg('rating_count'),
    sqlc.arg('is_active')
)
RETURNING id, name, slug, price, description, created_at, updated_at;

-- name: UpdateProduct :one
UPDATE products
SET name        = COALESCE(sqlc.narg('name'), name),
    slug        = COALESCE(sqlc.narg('slug'), slug),
    price       = COALESCE(sqlc.narg('price'), price),
    description = COALESCE(sqlc.narg('description'), description),
    category_id = COALESCE(
        (SELECT id FROM categories WHERE slug = sqlc.narg('category')::text),
        category_id
    ),
    brand_id = CASE
        WHEN sqlc.narg('brand')::text IS NULL THEN brand_id
        WHEN sqlc.narg('brand')::text = '' THEN NULL
        ELSE (SELECT id FROM brands WHERE slug = sqlc.narg('brand')::text)
    END,
    is_active  = COALESCE(sqlc.narg('is_active'), is_active),
    updated_at = now()
WHERE products.id = sqlc.arg('id')
RETURNING id, name, slug, price, description, created_at, updated_at;

-- name: DeleteProduct :execrows
DELETE FROM products WHERE id = sqlc.arg('id');
