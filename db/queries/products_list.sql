-- Daftar produk terpaginasikan (keyset). Satu query per `sort` supaya urutan
-- cocok dengan index dan cursor tetap stabil. `row_limit` = limit + 1 agar
-- pemanggil bisa mendeteksi halaman berikutnya.
--
-- Parameter filternya sama untuk keempat query:
--   include_inactive : bool (hanya dihormati untuk admin)
--   category         : text nullable (slug)
--   brand            : text nullable (slug)
--   search           : text nullable (substring nama)
--   row_limit        : int
-- Cursor (keyset) memakai perbandingan row-value, mis. (created_at, id) < (x, y).

-- name: ListProductsDefault :many
SELECT
    p.id, p.name, p.slug, p.price, p.rating_rate, p.rating_count, p.is_active, p.created_at,
    c.slug AS category,
    COALESCE(b.slug, '') AS brand,
    COALESCE((SELECT pi.key FROM product_images pi WHERE pi.product_id = p.id ORDER BY pi.position LIMIT 1), '')::text AS thumbnail
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE (sqlc.arg('include_inactive')::boolean OR p.is_active)
  AND (sqlc.narg('category')::text IS NULL OR c.slug = sqlc.narg('category')::text)
  AND (sqlc.narg('brand')::text IS NULL OR b.slug = sqlc.narg('brand')::text)
  AND (sqlc.narg('search')::text IS NULL OR p.name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (
      sqlc.narg('after_created_at')::timestamptz IS NULL
      OR (p.created_at, p.id) < (sqlc.narg('after_created_at')::timestamptz, sqlc.narg('after_id')::uuid)
  )
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: ListProductsPriceAsc :many
SELECT
    p.id, p.name, p.slug, p.price, p.rating_rate, p.rating_count, p.is_active, p.created_at,
    c.slug AS category,
    COALESCE(b.slug, '') AS brand,
    COALESCE((SELECT pi.key FROM product_images pi WHERE pi.product_id = p.id ORDER BY pi.position LIMIT 1), '')::text AS thumbnail
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE (sqlc.arg('include_inactive')::boolean OR p.is_active)
  AND (sqlc.narg('category')::text IS NULL OR c.slug = sqlc.narg('category')::text)
  AND (sqlc.narg('brand')::text IS NULL OR b.slug = sqlc.narg('brand')::text)
  AND (sqlc.narg('search')::text IS NULL OR p.name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (
      sqlc.narg('after_price')::bigint IS NULL
      OR (p.price, p.id) > (sqlc.narg('after_price')::bigint, sqlc.narg('after_id')::uuid)
  )
ORDER BY p.price ASC, p.id ASC
LIMIT sqlc.arg('row_limit')::int;

-- name: ListProductsPriceDesc :many
SELECT
    p.id, p.name, p.slug, p.price, p.rating_rate, p.rating_count, p.is_active, p.created_at,
    c.slug AS category,
    COALESCE(b.slug, '') AS brand,
    COALESCE((SELECT pi.key FROM product_images pi WHERE pi.product_id = p.id ORDER BY pi.position LIMIT 1), '')::text AS thumbnail
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE (sqlc.arg('include_inactive')::boolean OR p.is_active)
  AND (sqlc.narg('category')::text IS NULL OR c.slug = sqlc.narg('category')::text)
  AND (sqlc.narg('brand')::text IS NULL OR b.slug = sqlc.narg('brand')::text)
  AND (sqlc.narg('search')::text IS NULL OR p.name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (
      sqlc.narg('after_price')::bigint IS NULL
      OR (p.price, p.id) < (sqlc.narg('after_price')::bigint, sqlc.narg('after_id')::uuid)
  )
ORDER BY p.price DESC, p.id DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: ListProductsRatingDesc :many
SELECT
    p.id, p.name, p.slug, p.price, p.rating_rate, p.rating_count, p.is_active, p.created_at,
    c.slug AS category,
    COALESCE(b.slug, '') AS brand,
    COALESCE((SELECT pi.key FROM product_images pi WHERE pi.product_id = p.id ORDER BY pi.position LIMIT 1), '')::text AS thumbnail
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN brands b ON b.id = p.brand_id
WHERE (sqlc.arg('include_inactive')::boolean OR p.is_active)
  AND (sqlc.narg('category')::text IS NULL OR c.slug = sqlc.narg('category')::text)
  AND (sqlc.narg('brand')::text IS NULL OR b.slug = sqlc.narg('brand')::text)
  AND (sqlc.narg('search')::text IS NULL OR p.name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (
      sqlc.narg('after_rating_rate')::numeric IS NULL
      OR (p.rating_rate, p.rating_count, p.id) < (
          sqlc.narg('after_rating_rate')::numeric,
          sqlc.narg('after_rating_count')::integer,
          sqlc.narg('after_id')::uuid
      )
  )
ORDER BY p.rating_rate DESC, p.rating_count DESC, p.id DESC
LIMIT sqlc.arg('row_limit')::int;
