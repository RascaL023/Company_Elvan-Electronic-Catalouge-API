-- name: ListProductImages :many
SELECT product_id, position, key, file_id
FROM product_images
WHERE product_id = sqlc.arg('product_id')
ORDER BY position ASC;

-- name: InsertProductImage :exec
INSERT INTO product_images (product_id, position, key, file_id)
VALUES (
    sqlc.arg('product_id'),
    sqlc.arg('position'),
    sqlc.arg('key'),
    sqlc.narg('file_id')
);

-- name: DeleteProductImages :exec
DELETE FROM product_images WHERE product_id = sqlc.arg('product_id');
