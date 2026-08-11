package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const embeddingDimensions = 1536

type Embedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewEmbedder(apiKey, model, baseURL string, client *http.Client) (*Embedder, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("OpenAI embedding model is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("OpenAI base URL is required")
	}
	if client == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	return &Embedder{
		apiKey: apiKey, model: model, baseURL: strings.TrimRight(baseURL, "/"), client: client,
	}, nil
}

func (embedder *Embedder) Model() string { return embedder.model }

func (embedder *Embedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}
	body, err := json.Marshal(embeddingRequest{
		Model: embedder.model, Input: inputs, EncodingFormat: "float", Dimensions: embeddingDimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("encode embeddings request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		embedder.baseURL+"/embeddings",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create embeddings request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+embedder.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := embedder.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send embeddings request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("embeddings API returned %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}

	var payload embeddingResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if len(payload.Data) != len(inputs) {
		return nil, fmt.Errorf("embeddings API returned %d vectors for %d inputs", len(payload.Data), len(inputs))
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= len(inputs) || vectors[item.Index] != nil {
			return nil, fmt.Errorf("embeddings API returned invalid index %d", item.Index)
		}
		if len(item.Embedding) != embeddingDimensions {
			return nil, fmt.Errorf(
				"embeddings API returned %d dimensions at index %d, expected %d",
				len(item.Embedding),
				item.Index,
				embeddingDimensions,
			)
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
	Dimensions     int      `json:"dimensions"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}
