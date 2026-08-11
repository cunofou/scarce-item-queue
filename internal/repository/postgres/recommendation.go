package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Woolfer0097/GoodQueue/internal/pkg/domain"
	"github.com/Woolfer0097/GoodQueue/internal/pkg/repository"
	"github.com/google/uuid"
	"github.com/samber/oops"
)

const embeddingDimensions = 1536

type RecommendationRepository struct {
	db                         *sql.DB
	waitingBufferPercentSource domain.WaitingBufferPercentSource
}

type embeddingRefreshLease struct {
	connection *sql.Conn
	model      string
	once       sync.Once
	err        error
}

func NewRecommendationRepository(db *sql.DB, waitingBufferPercent int) *RecommendationRepository {
	return NewRecommendationRepositoryWithWaitingBufferPercentSource(
		db,
		domain.StaticWaitingBufferPercent(waitingBufferPercent),
	)
}

func NewRecommendationRepositoryWithWaitingBufferPercentSource(
	db *sql.DB,
	source domain.WaitingBufferPercentSource,
) *RecommendationRepository {
	return &RecommendationRepository{db: db, waitingBufferPercentSource: source}
}

func (r *RecommendationRepository) TryAcquireEmbeddingRefresh(
	ctx context.Context,
	model string,
) (repository.EmbeddingRefreshLease, bool, error) {
	connection, err := r.db.Conn(ctx)
	if err != nil {
		return nil, false, oops.Wrapf(err, "reserve embedding refresh connection")
	}
	var acquired bool
	if err := connection.QueryRowContext(ctx,
		`SELECT pg_try_advisory_lock(hashtextextended('goodqueue.embedding-refresh:' || $1, 0))`, model,
	).Scan(&acquired); err != nil {
		_ = connection.Close()
		return nil, false, oops.Wrapf(err, "acquire embedding refresh lease")
	}
	if !acquired {
		_ = connection.Close()
		return nil, false, nil
	}
	return &embeddingRefreshLease{connection: connection, model: model}, true, nil
}

func (lease *embeddingRefreshLease) Release() error {
	lease.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var released bool
		if err := lease.connection.QueryRowContext(ctx,
			`SELECT pg_advisory_unlock(hashtextextended('goodqueue.embedding-refresh:' || $1, 0))`, lease.model,
		).Scan(&released); err != nil {
			lease.err = oops.Wrapf(err, "release embedding refresh lease")
		} else if !released {
			lease.err = fmt.Errorf("release embedding refresh lease: lock was not held")
		}
		if err := lease.connection.Close(); err != nil && lease.err == nil {
			lease.err = oops.Wrapf(err, "close embedding refresh connection")
		}
	})
	return lease.err
}

func (r *RecommendationRepository) ListEmbeddingDocuments(
	ctx context.Context,
	model string,
) ([]domain.ProductEmbeddingDocument, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.title, p.description, p.category, p.price_cents, e.content_hash
		FROM products p
		LEFT JOIN product_embeddings e ON e.product_id = p.id AND e.model = $1
		ORDER BY p.id
	`, model)
	if err != nil {
		return nil, oops.Wrapf(err, "query product embedding documents")
	}
	defer func() { _ = rows.Close() }()

	documents := make([]domain.ProductEmbeddingDocument, 0)
	for rows.Next() {
		var id, title, description, category string
		var priceCents int64
		var cachedHash sql.NullString
		if err := rows.Scan(&id, &title, &description, &category, &priceCents, &cachedHash); err != nil {
			return nil, oops.Wrapf(err, "scan product embedding document")
		}
		productID, err := uuid.Parse(id)
		if err != nil {
			return nil, oops.Wrapf(err, "parse embedding product ID")
		}
		content := fmt.Sprintf(
			"Название: %s\nОписание: %s\nКатегория: %s\nЦена: %.2f RUB",
			title,
			description,
			category,
			float64(priceCents)/100,
		)
		digest := sha256.Sum256([]byte(content))
		contentHash := hex.EncodeToString(digest[:])
		if cachedHash.Valid && cachedHash.String == contentHash {
			continue
		}
		documents = append(documents, domain.ProductEmbeddingDocument{
			ProductID: domain.ProductID(productID), Content: content, ContentHash: contentHash,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Wrapf(err, "product embedding documents iteration")
	}
	return documents, nil
}

func (r *RecommendationRepository) UpsertEmbeddings(
	ctx context.Context,
	embeddings []domain.ProductEmbedding,
) error {
	if len(embeddings) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return oops.Wrapf(err, "begin product embeddings transaction")
	}
	defer func() { _ = tx.Rollback() }()

	for _, embedding := range embeddings {
		vector, err := formatVector(embedding.Vector)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO product_embeddings (product_id, model, content_hash, embedding, updated_at)
			VALUES ($1, $2, $3, $4::vector, NOW())
			ON CONFLICT (product_id) DO UPDATE SET
				model = EXCLUDED.model,
				content_hash = EXCLUDED.content_hash,
				embedding = EXCLUDED.embedding,
				updated_at = NOW()
		`, uuid.UUID(embedding.ProductID).String(), embedding.Model, embedding.ContentHash, vector); err != nil {
			return oops.Wrapf(err, "upsert product embedding")
		}
	}
	if err := tx.Commit(); err != nil {
		return oops.Wrapf(err, "commit product embeddings transaction")
	}
	return nil
}

func (r *RecommendationRepository) ListSemanticAlternatives(
	ctx context.Context,
	sourceID domain.ProductID,
	model string,
	limit int,
) ([]domain.ProductRecommendation, error) {
	if err := validateRecommendationLimit(limit); err != nil {
		return nil, err
	}
	return r.queryRecommendations(ctx, `
		WITH source AS (
			SELECT p.category, e.embedding
			FROM products p
			JOIN product_embeddings e ON e.product_id = p.id AND e.model = $2
			WHERE p.id = $1
		)
		SELECT p.id, p.title, p.description, p.image_url, p.category, p.price_cents,
		       p.queue_enabled, p.allocatable_stock, p.reserved, p.next_queue_sequence,
		       (SELECT COUNT(*) FROM queue_attempts q WHERE q.product_id = p.id AND q.state = 'waiting'),
		       GREATEST(0, LEAST(1,
		           0.75 * (1 - (e.embedding <=> source.embedding)) +
		           0.15 * CASE WHEN p.category = source.category THEN 1 ELSE 0 END +
		           0.10 * LEAST((p.allocatable_stock - p.reserved)::numeric / 3, 1)
		       ))::float8 AS score,
		       'semantically_similar'
		FROM products p
		JOIN product_embeddings e ON e.product_id = p.id AND e.model = $2
		CROSS JOIN source
		WHERE p.id <> $1 AND p.queue_enabled = TRUE AND p.allocatable_stock > p.reserved
		ORDER BY score DESC, p.id
		LIMIT $3
	`, sourceID, model, limit, domain.RecommendationModeSemantic)
}

func (r *RecommendationRepository) ListFallbackAlternatives(
	ctx context.Context,
	sourceID domain.ProductID,
	limit int,
) ([]domain.ProductRecommendation, error) {
	if err := validateRecommendationLimit(limit); err != nil {
		return nil, err
	}
	return r.queryRecommendations(ctx, `
		WITH source AS (
			SELECT category, price_cents FROM products WHERE id = $1
		)
		SELECT p.id, p.title, p.description, p.image_url, p.category, p.price_cents,
		       p.queue_enabled, p.allocatable_stock, p.reserved, p.next_queue_sequence,
		       (SELECT COUNT(*) FROM queue_attempts q WHERE q.product_id = p.id AND q.state = 'waiting'),
		       (
		           0.60 * CASE WHEN p.category = source.category THEN 1 ELSE 0 END +
		           0.25 * (1 - LEAST(ABS(p.price_cents - source.price_cents)::numeric / GREATEST(source.price_cents, 1), 1)) +
		           0.15 * LEAST((p.allocatable_stock - p.reserved)::numeric / 3, 1)
		       )::float8 AS score,
		       CASE WHEN p.category = source.category THEN 'same_category_available' ELSE 'available_now' END
		FROM products p
		CROSS JOIN source
		WHERE p.id <> $1 AND p.queue_enabled = TRUE AND p.allocatable_stock > p.reserved
		  AND (
		      p.category = source.category
		      OR NOT EXISTS (
		          SELECT 1
		          FROM products same_category
		          WHERE same_category.id <> $1
		            AND same_category.category = source.category
		            AND same_category.queue_enabled = TRUE
		            AND same_category.allocatable_stock > same_category.reserved
		      )
		  )
		ORDER BY score DESC, p.id
		LIMIT $2
	`, sourceID, "", limit, domain.RecommendationModeFallback)
}

func (r *RecommendationRepository) queryRecommendations(
	ctx context.Context,
	query string,
	sourceID domain.ProductID,
	model string,
	limit int,
	mode domain.RecommendationMode,
) ([]domain.ProductRecommendation, error) {
	var rows *sql.Rows
	var err error
	if model == "" {
		rows, err = r.db.QueryContext(ctx, query, uuid.UUID(sourceID).String(), limit)
	} else {
		rows, err = r.db.QueryContext(ctx, query, uuid.UUID(sourceID).String(), model, limit)
	}
	if err != nil {
		return nil, oops.Wrapf(err, "query %s recommendations", mode)
	}
	defer func() { _ = rows.Close() }()

	recommendations := make([]domain.ProductRecommendation, 0, limit)
	percent := r.waitingBufferPercentSource.CurrentWaitingBufferPercent()
	for rows.Next() {
		var item domain.ProductRecommendation
		var id, reason string
		if err := rows.Scan(
			&id, &item.Product.Title, &item.Product.Description, &item.Product.ImageURL,
			&item.Product.Category, &item.Product.PriceCents, &item.Product.QueueEnabled,
			&item.Product.AllocatableStock, &item.Product.Reserved, &item.Product.NextQueueSequence,
			&item.Product.WaitingCount, &item.Score, &reason,
		); err != nil {
			return nil, oops.Wrapf(err, "scan %s recommendation", mode)
		}
		productID, err := uuid.Parse(id)
		if err != nil {
			return nil, oops.Wrapf(err, "parse recommendation product ID")
		}
		item.Product.ID = domain.ProductID(productID)
		item.Product.WaitingCapacity, err = domain.WaitingCapacity(
			item.Product.AllocatableStock,
			percent,
		)
		if err != nil {
			return nil, oops.Wrapf(err, "calculate recommendation waiting capacity")
		}
		item.Mode = mode
		item.ReasonCode = domain.RecommendationReason(reason)
		recommendations = append(recommendations, item)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Wrapf(err, "%s recommendations iteration", mode)
	}
	return recommendations, nil
}

func validateRecommendationLimit(limit int) error {
	if limit < 1 || limit > 20 {
		return domain.ErrInvalidInput
	}
	return nil
}

func formatVector(values []float32) (string, error) {
	if len(values) != embeddingDimensions {
		return "", fmt.Errorf("embedding must contain %d dimensions, got %d", embeddingDimensions, len(values))
	}
	parts := make([]string, len(values))
	for index, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", fmt.Errorf("embedding contains a non-finite value at index %d", index)
		}
		parts[index] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}
