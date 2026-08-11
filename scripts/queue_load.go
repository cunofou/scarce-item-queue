//go:build ignore

// queue_load verifies the public HTTP queue contract under concurrent joins.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type queueResponse struct {
	AttemptID string `json:"attempt_id"`
	State     string `json:"state"`
}

type productResponse struct {
	AllocatableStock int32 `json:"allocatable_stock"`
	Reserved         int32 `json:"reserved"`
	FreeStock        int32 `json:"free_stock"`
}

type errorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

type joinResult struct {
	index      int
	userID     string
	key        string
	statusCode int
	queue      queueResponse
	errorCode  string
	err        error
}

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "GoodQueue HTTP base URL")
	productID := flag.String("product-id", "11111111-1111-1111-1111-111111111111", "stock=1 demo product UUID")
	requests := flag.Int("requests", 20, "number of concurrent users")
	timeout := flag.Duration("timeout", 15*time.Second, "overall request timeout")
	flag.Parse()

	if *requests < 2 || *requests > 1000 {
		fatalf("requests must be between 2 and 1000")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client := &http.Client{Timeout: *timeout}

	before, err := getProduct(ctx, client, *baseURL, *productID)
	if err != nil {
		fatalf("read initial product: %v", err)
	}
	if before.AllocatableStock != 1 || before.Reserved != 0 {
		fatalf("load test requires a fresh stock=1 product; got stock=%d reserved=%d (run docker compose down --volumes, then compose-up)", before.AllocatableStock, before.Reserved)
	}

	start := make(chan struct{})
	results := make(chan joinResult, *requests)
	var workers sync.WaitGroup
	for index := 1; index <= *requests; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			userID := fmt.Sprintf("10000000-0000-4000-8000-%012d", index)
			key := fmt.Sprintf("load-join-%d", index)
			result := join(ctx, client, *baseURL, *productID, userID, key)
			result.index = index
			result.userID = userID
			result.key = key
			results <- result
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)

	allocated := 0
	waiting := 0
	queueFull := 0
	var replayCandidate *joinResult
	for result := range results {
		if result.err != nil {
			fatalf("join %d: %v", result.index, result.err)
		}
		switch {
		case result.statusCode == http.StatusCreated || result.statusCode == http.StatusOK:
			if result.queue.AttemptID == "" {
				fatalf("join %d returned no attempt_id", result.index)
			}
			if replayCandidate == nil {
				copy := result
				replayCandidate = &copy
			}
			switch result.queue.State {
			case "invited", "checkout":
				allocated++
			case "waiting":
				waiting++
			default:
				fatalf("join %d returned unexpected state %q", result.index, result.queue.State)
			}
		case result.statusCode == http.StatusConflict && result.errorCode == "queue_full":
			queueFull++
		default:
			fatalf("join %d returned HTTP %d code %q", result.index, result.statusCode, result.errorCode)
		}
	}

	if allocated != 1 {
		fatalf("concurrency invariant failed: got %d allocated attempts, want exactly 1", allocated)
	}
	if replayCandidate == nil {
		fatalf("no successful join available for idempotency replay")
	}
	replay := join(ctx, client, *baseURL, *productID, replayCandidate.userID, replayCandidate.key)
	if replay.err != nil || replay.statusCode != http.StatusOK || replay.queue.AttemptID != replayCandidate.queue.AttemptID {
		fatalf("idempotency replay failed: status=%d attempt=%q err=%v", replay.statusCode, replay.queue.AttemptID, replay.err)
	}

	after, err := getProduct(ctx, client, *baseURL, *productID)
	if err != nil {
		fatalf("read final product: %v", err)
	}
	if after.Reserved != 1 || after.Reserved > after.AllocatableStock || after.FreeStock != 0 {
		fatalf("inventory invariant failed: stock=%d reserved=%d free=%d", after.AllocatableStock, after.Reserved, after.FreeStock)
	}

	fmt.Printf("PASS concurrent queue joins: requests=%d allocated=%d waiting=%d queue_full=%d reserved=%d\n", *requests, allocated, waiting, queueFull, after.Reserved)
}

func join(ctx context.Context, client *http.Client, baseURL, productID, userID, key string) joinResult {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/products/"+productID+"/queue-entries", bytes.NewReader(nil))
	if err != nil {
		return joinResult{err: err}
	}
	request.Header.Set("X-User-ID", userID)
	request.Header.Set("Idempotency-Key", key)
	response, err := client.Do(request)
	if err != nil {
		return joinResult{err: err}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return joinResult{err: err}
	}
	result := joinResult{statusCode: response.StatusCode}
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated {
		result.err = json.Unmarshal(body, &result.queue)
		return result
	}
	var failure errorResponse
	if err := json.Unmarshal(body, &failure); err != nil {
		result.err = fmt.Errorf("decode HTTP %d response: %w", response.StatusCode, err)
		return result
	}
	result.errorCode = failure.Error.Code
	return result
}

func getProduct(ctx context.Context, client *http.Client, baseURL, productID string) (productResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/products/"+productID, nil)
	if err != nil {
		return productResponse{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return productResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return productResponse{}, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var product productResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&product); err != nil {
		return productResponse{}, err
	}
	return product, nil
}

func fatalf(format string, arguments ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", arguments...)
	os.Exit(1)
}
