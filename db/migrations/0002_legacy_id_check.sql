-- +goose Up
-- Issue #6: legacy_id (id Firestore) unik sejak 0001; di sini dilengkapi
-- aturan bahwa string kosong tidak diizinkan. NULL tetap sah untuk produk/
-- kategori/brand yang dibuat native tanpa leluhur Firestore.
--
-- Nilai selain string kosong diterima apa adanya: migration ini tidak
-- melakukan trimming atau normalisasi (mis. ' abc ' tetap sah).

ALTER TABLE products
  DROP CONSTRAINT IF EXISTS products_legacy_id_nonempty,
  ADD CONSTRAINT products_legacy_id_nonempty CHECK (legacy_id <> '');

ALTER TABLE categories
  DROP CONSTRAINT IF EXISTS categories_legacy_id_nonempty,
  ADD CONSTRAINT categories_legacy_id_nonempty CHECK (legacy_id <> '');

ALTER TABLE brands
  DROP CONSTRAINT IF EXISTS brands_legacy_id_nonempty,
  ADD CONSTRAINT brands_legacy_id_nonempty CHECK (legacy_id <> '');

-- +goose Down
ALTER TABLE products  DROP CONSTRAINT IF EXISTS products_legacy_id_nonempty;
ALTER TABLE categories DROP CONSTRAINT IF EXISTS categories_legacy_id_nonempty;
ALTER TABLE brands    DROP CONSTRAINT IF EXISTS brands_legacy_id_nonempty;
