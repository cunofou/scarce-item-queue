package domain

type RecommendationMode string

const (
	RecommendationModeSemantic RecommendationMode = "ai_semantic"
	RecommendationModeFallback RecommendationMode = "catalog_fallback"
)

type RecommendationReason string

const (
	RecommendationReasonSemantic     RecommendationReason = "semantically_similar"
	RecommendationReasonSameCategory RecommendationReason = "same_category_available"
	RecommendationReasonAvailable    RecommendationReason = "available_now"
)

type ProductRecommendation struct {
	Product    Product
	Score      float64
	Mode       RecommendationMode
	ReasonCode RecommendationReason
}

type ProductEmbeddingDocument struct {
	ProductID   ProductID
	Content     string
	ContentHash string
}

type ProductEmbedding struct {
	ProductID   ProductID
	Model       string
	ContentHash string
	Vector      []float32
}
