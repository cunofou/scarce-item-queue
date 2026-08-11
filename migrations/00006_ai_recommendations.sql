-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE products
    ADD COLUMN category TEXT NOT NULL DEFAULT 'other',
    ADD COLUMN price_cents BIGINT NOT NULL DEFAULT 0 CHECK (price_cents >= 0);

CREATE TABLE product_embeddings (
    product_id UUID PRIMARY KEY REFERENCES products (id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    embedding vector(1536) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX product_embeddings_cosine_hnsw_idx
    ON product_embeddings USING hnsw (embedding vector_cosine_ops);

-- +goose Down
DROP TABLE product_embeddings;
ALTER TABLE products DROP COLUMN price_cents, DROP COLUMN category;
DROP EXTENSION vector;
