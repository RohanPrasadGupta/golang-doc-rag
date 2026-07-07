package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
)

const voyageEmbeddingsURL = "https://api.voyageai.com/v1/embeddings"

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

func EmbedTexts(ctx context.Context, texts []string) ([][]float64, error) {
	vougeApi := os.Getenv("VOYAGE_API_KEY")

	if vougeApi == "" {
		return nil, errors.New("VOYAGE_API_KEY is not set")
	}

	requestBody := voyageEmbeddingRequest{
		Input:     texts,
		Model:     "voyage-4-lite",
		InputType: "document",
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

	req.Header.Set("Authorization", "Bearer "+vougeApi)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
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

	embeddings := make([][]float64, len(response.Data))

	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(embeddings) {
			return nil, fmt.Errorf(
				"invalid embedding index returned: %d",
				item.Index,
			)
		}

		embeddings[item.Index] = item.Embedding
	}
	// log.Printf("embedded %d chunks, first vector has %d dimensions", len(embeddings), len(embeddings[0]))
	// log.Printf("first vector: %v", embeddings[0])

	return embeddings, nil
}
