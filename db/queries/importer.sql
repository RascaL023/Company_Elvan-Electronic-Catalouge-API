-- Query khusus cmd/importer (Fase 6).
--
-- Impor bersifat IDEMPOTEN: baris dicocokkan lewat `legacy_id` (id Firestore).
-- Kalau `legacy_id` sudah ada, baris diperbarui; kalau belum, baris baru dibuat.
-- Satu statement (INSERT ... ON CONFLICT) per entitas sehingga aman dijalankan
-- berulang tanpa risiko duplikat akibat baca-lalu-tulis.
--
-- `created_at` dipertahankan dari dokumen sumber supaya urutan `GET /catalog`
-- (created_at DESC) identik dengan Firestore; bila tidak disediakan, memakai
-- now().
--
-- category_id/brand_id di-resolve dari slug lewat subquery; brand kosong menjadi
-- NULL. Baris yang rujukannya hilang jadi NOT NULL violation -> ErrValidation.

-- name: UpsertCategoryImport :one
INSERT INTO categories (name, slug, description, legacy_id, created_at)
VALUES (
    sqlc.arg('name'),
    sqlc.arg('slug'),
    sqlc.arg('description'),
    sqlc.arg('legacy_id'),
    COALESCE(sqlc.narg('created_at'), now())
)
ON CONFLICT (legacy_id) DO UPDATE
SET name        = EXCLUDED.name,
    slug        = EXCLUDED.slug,
    description = EXCLUDED.description,
    updated_at  = now()
RETURNING id, name, slug, description, created_at, updated_at;

-- name: UpsertBrandImport :one
INSERT INTO brands (name, slug, legacy_id, created_at)
VALUES (
    sqlc.arg('name'),
    sqlc.arg('slug'),
    sqlc.arg('legacy_id'),
    COALESCE(sqlc.narg('created_at'), now())
)
ON CONFLICT (legacy_id) DO UPDATE
SET name       = EXCLUDED.name,
    slug       = EXCLUDED.slug,
    updated_at = now()
RETURNING id, name, slug, created_at, updated_at;

-- name: UpsertProductImport :one
-- category/brand diberikan sebagai slug; subquery me-resolve ke foreign key.
-- brand kosong -> NULL. legacy_id unik -> impor idempoten.
INSERT INTO products (
    name, slug, price, description,
    category_id, brand_id,
    rating_rate, rating_count, is_active, legacy_id, created_at
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
    sqlc.arg('is_active'),
    sqlc.arg('legacy_id'),
    COALESCE(sqlc.narg('created_at'), now())
)
ON CONFLICT (legacy_id) DO UPDATE
SET name         = EXCLUDED.name,
    slug         = EXCLUDED.slug,
    price        = EXCLUDED.price,
    description  = EXCLUDED.description,
    category_id  = EXCLUDED.category_id,
    brand_id     = EXCLUDED.brand_id,
    rating_rate  = EXCLUDED.rating_rate,
    rating_count = EXCLUDED.rating_count,
    is_active    = EXCLUDED.is_active,
    updated_at   = now()
RETURNING id, name, slug, price, description, created_at, updated_at;

-- name: CountProducts :one
SELECT count(*) FROM products;

-- name: CountCategories :one
SELECT count(*) FROM categories;

-- name: CountBrands :one
SELECT count(*) FROM brands;
