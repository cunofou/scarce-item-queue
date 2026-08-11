package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/Woolfer0097/GoodQueue/internal/pkg/domain"
	"github.com/Woolfer0097/GoodQueue/internal/pkg/repository"
)

type productRepositoryStub struct {
	getErr error
}

func (stub *productRepositoryStub) List(context.Context) ([]domain.Product, error) {
	return nil, nil
}

func (stub *productRepositoryStub) Get(context.Context, domain.ProductID) (domain.Product, error) {
	return domain.Product{}, stub.getErr
}

type recommendationRepositoryStub struct {
	documents     []domain.ProductEmbeddingDocument
	semantic      []domain.ProductRecommendation
	fallback      []domain.ProductRecommendation
	fallbackCalls int
	semanticCalls int
	upserts       []domain.ProductEmbedding
	limit         int
	leaseAcquired bool
}

type embeddingLeaseStub struct{}

func (embeddingLeaseStub) Release() error { return nil }

func (stub *recommendationRepositoryStub) TryAcquireEmbeddingRefresh(
	context.Context,
	string,
) (repository.EmbeddingRefreshLease, bool, error) {
	return embeddingLeaseStub{}, stub.leaseAcquired, nil
}

func (stub *recommendationRepositoryStub) ListEmbeddingDocuments(
	context.Context,
	string,
) ([]domain.ProductEmbeddingDocument, error) {
	return stub.documents, nil
}

func (stub *recommendationRepositoryStub) UpsertEmbeddings(
	_ context.Context,
	embeddings []domain.ProductEmbedding,
) error {
	stub.upserts = embeddings
	return nil
}

func (stub *recommendationRepositoryStub) ListSemanticAlternatives(
	_ context.Context,
	_ domain.ProductID,
	_ string,
	limit int,
) ([]domain.ProductRecommendation, error) {
	stub.semanticCalls++
	stub.limit = limit
	return stub.semantic, nil
}

func (stub *recommendationRepositoryStub) ListFallbackAlternatives(
	_ context.Context,
	_ domain.ProductID,
	limit int,
) ([]domain.ProductRecommendation, error) {
	stub.fallbackCalls++
	stub.limit = limit
	return stub.fallback, nil
}

type embeddingProviderStub struct {
	vectors [][]float32
	err     error
	calls   int
}

func (stub *embeddingProviderStub) Model() string { return "test-embedding-model" }

func (stub *embeddingProviderStub) Embed(context.Context, []string) ([][]float32, error) {
	stub.calls++
	return stub.vectors, stub.err
}

func TestProductAlternativesRequireExistingSourceProduct(t *testing.T) {
	repository := &productRepositoryStub{getErr: domain.ErrNotFound}
	recommendations := &recommendationRepositoryStub{}
	_, err := NewProductUseCase(repository, recommendations, nil).Alternatives(context.Background(), domain.ProductID{})
	if !errors.Is(err, domain.ErrNotFound) || recommendations.fallbackCalls != 0 {
		t.Fatalf("missing source product: calls=%d err=%v", recommendations.fallbackCalls, err)
	}
}

func TestProductAlternativesUseBoundedMVPResult(t *testing.T) {
	want := []domain.ProductRecommendation{{Product: domain.Product{Title: "alternative"}}}
	recommendations := &recommendationRepositoryStub{fallback: want}
	got, err := NewProductUseCase(&productRepositoryStub{}, recommendations, nil).Alternatives(
		context.Background(),
		domain.ProductID{},
	)
	if err != nil || len(got) != 1 || got[0].Product.Title != want[0].Product.Title {
		t.Fatalf("alternatives result: %#v err=%v", got, err)
	}
	if recommendations.fallbackCalls != 1 || recommendations.limit != 4 {
		t.Fatalf("alternatives query: calls=%d limit=%d", recommendations.fallbackCalls, recommendations.limit)
	}
}

func TestProductAlternativesCachesEmbeddingsAndUsesSemanticResults(t *testing.T) {
	recommendations := &recommendationRepositoryStub{
		documents: []domain.ProductEmbeddingDocument{{Content: "product", ContentHash: "hash"}},
		semantic: []domain.ProductRecommendation{{
			Product: domain.Product{Title: "semantic"}, Mode: domain.RecommendationModeSemantic,
		}},
		leaseAcquired: true,
	}
	provider := &embeddingProviderStub{vectors: [][]float32{make([]float32, 1536)}}
	got, err := NewProductUseCase(&productRepositoryStub{}, recommendations, provider).Alternatives(
		context.Background(),
		domain.ProductID{},
	)
	if err != nil || len(got) != 1 || got[0].Product.Title != "semantic" {
		t.Fatalf("semantic alternatives: %#v err=%v", got, err)
	}
	if provider.calls != 1 || len(recommendations.upserts) != 1 || recommendations.fallbackCalls != 0 {
		t.Fatalf("embedding flow: provider=%d upserts=%d fallback=%d", provider.calls, len(recommendations.upserts), recommendations.fallbackCalls)
	}
}

func TestProductAlternativesFallsBackWhenEmbeddingProviderFails(t *testing.T) {
	recommendations := &recommendationRepositoryStub{
		documents:     []domain.ProductEmbeddingDocument{{Content: "product"}},
		fallback:      []domain.ProductRecommendation{{Product: domain.Product{Title: "fallback"}}},
		leaseAcquired: true,
	}
	provider := &embeddingProviderStub{err: errors.New("provider unavailable")}
	got, err := NewProductUseCase(&productRepositoryStub{}, recommendations, provider).Alternatives(
		context.Background(),
		domain.ProductID{},
	)
	if err != nil || len(got) != 1 || got[0].Product.Title != "fallback" {
		t.Fatalf("fallback alternatives: %#v err=%v", got, err)
	}
	if recommendations.semanticCalls != 1 || recommendations.fallbackCalls != 1 {
		t.Fatalf("fallback flow: semantic=%d fallback=%d", recommendations.semanticCalls, recommendations.fallbackCalls)
	}
}

func TestProductAlternativesSkipsDuplicateEmbeddingRefreshWhenLeaseIsBusy(t *testing.T) {
	recommendations := &recommendationRepositoryStub{
		fallback: []domain.ProductRecommendation{{Product: domain.Product{Title: "fallback"}}},
	}
	provider := &embeddingProviderStub{}
	got, err := NewProductUseCase(&productRepositoryStub{}, recommendations, provider).Alternatives(
		context.Background(), domain.ProductID{},
	)
	if err != nil || len(got) != 1 || got[0].Product.Title != "fallback" {
		t.Fatalf("fallback while refresh is leased: %#v err=%v", got, err)
	}
	if provider.calls != 0 || len(recommendations.upserts) != 0 {
		t.Fatalf("busy lease called embedding provider=%d upserts=%d", provider.calls, len(recommendations.upserts))
	}
}
