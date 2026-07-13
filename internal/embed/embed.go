package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	voyageEmbeddingsURL = "https://api.voyageai.com/v1/embeddings"
	defaultModel        = "voyage-4-lite"
	maxBatchSize        = 128
)

const (
	InputTypeDocument = "document"
	InputTypeQuery    = "query"
)

var httpClient = &http.Client{
	Timeout: 60 * time.Second,
}

type voyageEmbeddingRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"`
}

type voyageEmbeddingData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type voyageEmbeddingResponse struct {
	Data []voyageEmbeddingData `json:"data"`
}

// EmbedTexts embeds texts with Voyage. inputType should be InputTypeDocument or InputTypeQuery.
func EmbedTexts(ctx context.Context, texts []string, inputType string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	voyageAPIKey := os.Getenv("VOYAGE_API_KEY")
	if voyageAPIKey == "" {
		return nil, errors.New("VOYAGE_API_KEY is not set")
	}

	if inputType == "" {
		inputType = InputTypeDocument
	}

	all := make([][]float64, len(texts))

	for start := 0; start < len(texts); start += maxBatchSize {
		end := start + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch, err := embedBatch(ctx, voyageAPIKey, texts[start:end], inputType)
		if err != nil {
			return nil, err
		}

		copy(all[start:end], batch)
	}

	return all, nil
}

func embedBatch(ctx context.Context, apiKey string, texts []string, inputType string) ([][]float64, error) {
	requestBody := voyageEmbeddingRequest{
		Input:     texts,
		Model:     defaultModel,
		InputType: inputType,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		voyageEmbeddingsURL,
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"voyage embedding request failed with status: %s",
			resp.Status,
		)
	}

	var response voyageEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	embeddings := make([][]float64, len(texts))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(embeddings) {
			return nil, fmt.Errorf(
				"invalid embedding index returned: %d",
				item.Index,
			)
		}
		embeddings[item.Index] = item.Embedding
	}

	for i, emb := range embeddings {
		if emb == nil {
			return nil, fmt.Errorf("missing embedding for index %d", i)
		}
	}

	return embeddings, nil
}
