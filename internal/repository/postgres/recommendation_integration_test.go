package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/Woolfer0097/GoodQueue/internal/pkg/domain"
	"github.com/google/uuid"
)

func TestIntegrationEmbeddingRefreshLeaseIsGlobalPerModel(t *testing.T) {
	database := openIntegrationDatabase(t)
	firstRepository := NewRecommendationRepository(database, 100)
	secondRepository := NewRecommendationRepository(database, 100)
	const model = "lease-integration-model"

	firstLease, acquired, err := firstRepository.TryAcquireEmbeddingRefresh(context.Background(), model)
	if err != nil || !acquired {
		t.Fatalf("acquire first embedding lease: acquired=%t err=%v", acquired, err)
	}
	defer func() { _ = firstLease.Release() }()

	secondLease, acquired, err := secondRepository.TryAcquireEmbeddingRefresh(context.Background(), model)
	if err != nil || acquired || secondLease != nil {
		t.Fatalf("concurrent embedding lease: lease=%v acquired=%t err=%v", secondLease, acquired, err)
	}
	if err := firstLease.Release(); err != nil {
		t.Fatal(err)
	}
	secondLease, acquired, err = secondRepository.TryAcquireEmbeddingRefresh(context.Background(), model)
	if err != nil || !acquired {
		t.Fatalf("reacquire embedding lease: acquired=%t err=%v", acquired, err)
	}
	if err := secondLease.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationSemanticRecommendationsRankNearestAvailableProduct(t *testing.T) {
	database := openIntegrationDatabase(t)
	repository := NewRecommendationRepository(database, 100)
	const model = "integration-embedding-model"
	t.Cleanup(func() {
		if _, err := database.Exec(`DELETE FROM product_embeddings WHERE model = $1`, model); err != nil {
			t.Errorf("clean recommendation embeddings: %v", err)
		}
	})

	sourceID := mustProductID(t, integrationProductOne)
	nearestID := mustProductID(t, "44444444-4444-4444-4444-444444444444")
	distantID := mustProductID(t, "77777777-7777-7777-7777-777777777777")
	if _, err := database.Exec(
		`UPDATE products SET reserved = 0 WHERE id IN ($1, $2, $3)`,
		uuid.UUID(sourceID),
		uuid.UUID(nearestID),
		uuid.UUID(distantID),
	); err != nil {
		t.Fatal(err)
	}

	sourceVector := make([]float32, embeddingDimensions)
	sourceVector[0] = 1
	nearestVector := make([]float32, embeddingDimensions)
	nearestVector[0] = 1
	distantVector := make([]float32, embeddingDimensions)
	distantVector[1] = 1
	if err := repository.UpsertEmbeddings(context.Background(), []domain.ProductEmbedding{
		{ProductID: sourceID, Model: model, ContentHash: strings.Repeat("1", 64), Vector: sourceVector},
		{ProductID: nearestID, Model: model, ContentHash: strings.Repeat("2", 64), Vector: nearestVector},
		{ProductID: distantID, Model: model, ContentHash: strings.Repeat("3", 64), Vector: distantVector},
	}); err != nil {
		t.Fatal(err)
	}

	recommendations, err := repository.ListSemanticAlternatives(
		context.Background(),
		sourceID,
		model,
		4,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations) != 2 {
		t.Fatalf("semantic recommendations: got %d, want 2", len(recommendations))
	}
	if recommendations[0].Product.ID != nearestID || recommendations[0].Score <= recommendations[1].Score {
		t.Fatalf("nearest product was not ranked first: %+v", recommendations)
	}
	if recommendations[0].Mode != domain.RecommendationModeSemantic ||
		recommendations[0].ReasonCode != domain.RecommendationReasonSemantic {
		t.Fatalf("unexpected recommendation metadata: %+v", recommendations[0])
	}
}

func TestIntegrationFallbackRecommendationsExcludeSourceAndUnavailableProducts(t *testing.T) {
	database := openIntegrationDatabase(t)
	repository := NewRecommendationRepository(database, 100)
	sourceID := mustProductID(t, integrationProductOne)
	var sourceCategory string
	if err := database.QueryRow(
		`SELECT category FROM products WHERE id=$1`, uuid.UUID(sourceID),
	).Scan(&sourceCategory); err != nil {
		t.Fatal(err)
	}
	recommendations, err := repository.ListFallbackAlternatives(context.Background(), sourceID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations) == 0 {
		t.Fatal("fallback returned no available products")
	}
	for _, recommendation := range recommendations {
		if recommendation.Product.ID == sourceID || recommendation.Product.FreeStock() <= 0 {
			t.Fatalf("invalid fallback recommendation: %+v", recommendation)
		}
		if recommendation.Mode != domain.RecommendationModeFallback {
			t.Fatalf("unexpected fallback mode: %s", recommendation.Mode)
		}
		if recommendation.Product.Category != sourceCategory ||
			recommendation.ReasonCode != domain.RecommendationReasonSameCategory {
			t.Fatalf("fallback ignored available same-category products: %+v", recommendation)
		}
	}
}

func TestIntegrationFallbackRecommendationsUseAvailableProductsWhenCategoryIsExhausted(t *testing.T) {
	database := openIntegrationDatabase(t)
	repository := NewRecommendationRepository(database, 100)
	sourceID := mustProductID(t, integrationProductOne)

	var originalCategory string
	if err := database.QueryRow(
		`SELECT category FROM products WHERE id=$1`, uuid.UUID(sourceID),
	).Scan(&originalCategory); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE products SET category='integration-category-without-alternatives' WHERE id=$1`,
		uuid.UUID(sourceID),
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := database.Exec(
			`UPDATE products SET category=$1 WHERE id=$2`, originalCategory, uuid.UUID(sourceID),
		); err != nil {
			t.Errorf("restore source product category: %v", err)
		}
	})

	recommendations, err := repository.ListFallbackAlternatives(context.Background(), sourceID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations) == 0 {
		t.Fatal("fallback returned no generally available products")
	}
	for _, recommendation := range recommendations {
		if recommendation.ReasonCode != domain.RecommendationReasonAvailable {
			t.Fatalf("unexpected exhausted-category recommendation: %+v", recommendation)
		}
	}
}
