package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEmbedderSendsBatchAndRestoresAPIOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected authorization header")
		}
		var payload embeddingRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "text-embedding-3-small" || payload.Dimensions != 1536 || len(payload.Input) != 2 {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]any{
			{"index": 1, "embedding": vectorWithFirstValue(2)},
			{"index": 0, "embedding": vectorWithFirstValue(1)},
		}})
	}))
	defer server.Close()

	embedder, err := NewEmbedder(
		"secret",
		"text-embedding-3-small",
		server.URL+"/v1",
		&http.Client{Timeout: time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := embedder.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 2 {
		t.Fatalf("vectors are not in input order: first=%v second=%v", vectors[0][0], vectors[1][0])
	}
}

func TestEmbedderReturnsBoundedProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()
	embedder, err := NewEmbedder("secret", "model", server.URL, &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = embedder.Embed(context.Background(), []string{"product"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected provider status error, got %v", err)
	}
}

func vectorWithFirstValue(value float32) []float32 {
	vector := make([]float32, embeddingDimensions)
	vector[0] = value
	return vector
}
