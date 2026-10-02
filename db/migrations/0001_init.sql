-- +goose Up
-- Skema awal sesuai docs/ARCHITECTURE.md §9.1.
-- Fitur Postgres 18 yang dipakai: uuidv7(), pg_trgm.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE categories (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  name        text        NOT NULL,
  slug        text        NOT NULL UNIQUE,
  description text        NOT NULL DEFAULT '',
  legacy_id   text        UNIQUE,              -- id Firestore (hanya untuk migrasi)
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE brands (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  name        text        NOT NULL,
  slug        text        NOT NULL UNIQUE,
  legacy_id   text        UNIQUE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE products (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  name         text        NOT NULL,
  slug         text        NOT NULL UNIQUE,
  price        bigint      NOT NULL CHECK (price >= 0),
  description  text        NOT NULL DEFAULT '',
  category_id  uuid        NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
  brand_id     uuid                 REFERENCES brands(id)     ON DELETE RESTRICT,
  rating_rate  numeric(2,1) NOT NULL DEFAULT 0 CHECK (rating_rate BETWEEN 0 AND 5),
  rating_count integer     NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
  is_active    boolean     NOT NULL DEFAULT true,
  legacy_id    text        UNIQUE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE product_images (
  product_id uuid     NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  position   smallint NOT NULL CHECK (position >= 0),
  key        text     NOT NULL,        -- relative key, tanpa slash di depan
  file_id    text,                     -- NULL untuk gambar lama tanpa file id provider
  PRIMARY KEY (product_id, position)
);

CREATE TABLE admins (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  email         text        NOT NULL UNIQUE,
  password_hash text        NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  admin_id     uuid        NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
  token_hash   bytea       NOT NULL UNIQUE,   -- SHA-256 dari token; token mentah tidak disimpan
  expires_at   timestamptz NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);

-- Indeks untuk query katalog (hanya produk aktif untuk jalur publik)
CREATE INDEX products_newest_idx      ON products (is_active, created_at DESC, id DESC);
CREATE INDEX products_category_idx    ON products (category_id, is_active, created_at DESC, id DESC);
CREATE INDEX products_price_idx       ON products (is_active, price, id);
CREATE INDEX products_rating_idx      ON products (is_active, rating_rate DESC, rating_count DESC, id DESC);
CREATE INDEX products_name_trgm_idx   ON products USING gin (name gin_trgm_ops);
CREATE INDEX sessions_expires_idx     ON sessions (expires_at);

-- +goose Down
-- Urutan dibalik agar foreign key tidak menghalangi.
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS admins;
DROP TABLE IF EXISTS product_images;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS brands;
DROP TABLE IF EXISTS categories;
