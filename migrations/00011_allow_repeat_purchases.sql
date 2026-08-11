-- +goose Up
DROP INDEX queue_attempts_one_purchase_per_user_product_idx;

-- +goose Down
CREATE UNIQUE INDEX queue_attempts_one_purchase_per_user_product_idx
    ON queue_attempts (external_user_id, product_id)
    WHERE state = 'purchased';
