package usecase

import "context"

type EmbeddingProvider interface {
	Model() string
	Embed(context.Context, []string) ([][]float32, error)
}
