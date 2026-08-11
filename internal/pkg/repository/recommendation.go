package repository

import (
	"context"

	"github.com/Woolfer0097/GoodQueue/internal/pkg/domain"
)

type EmbeddingRefreshLease interface {
	Release() error
}

type Recommendation interface {
	TryAcquireEmbeddingRefresh(context.Context, string) (EmbeddingRefreshLease, bool, error)
	ListEmbeddingDocuments(context.Context, string) ([]domain.ProductEmbeddingDocument, error)
	UpsertEmbeddings(context.Context, []domain.ProductEmbedding) error
	ListSemanticAlternatives(context.Context, domain.ProductID, string, int) ([]domain.ProductRecommendation, error)
	ListFallbackAlternatives(context.Context, domain.ProductID, int) ([]domain.ProductRecommendation, error)
}
