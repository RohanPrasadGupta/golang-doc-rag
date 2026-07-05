package vectordb

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/pinecone-io/go-pinecone/v3/pinecone"
	"google.golang.org/protobuf/types/known/structpb"
)

type PineconeStore struct {
	client *pinecone.Client
	index  *pinecone.IndexConnection
}

func NewPinecone(ctx context.Context) (*PineconeStore, error) {
	pineAPIKey := os.Getenv("PINECONE_API_KEY")
	if pineAPIKey == "" {
		return nil, fmt.Errorf("PINECONE_API_KEY is not set")
	}

	pineHost := os.Getenv("PINECONE_HOST")
	if pineHost == "" {
		return nil, fmt.Errorf("PINECONE_HOST is not set")
	}

	client, err := pinecone.NewClient(pinecone.NewClientParams{
		ApiKey: pineAPIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create Pinecone client: %w", err)
	}

	index, err := client.Index(pinecone.NewIndexConnParams{Host: pineHost})
	if err != nil {
		return nil, fmt.Errorf("connect to Pinecone index: %w", err)
	}

	stats, err := index.DescribeIndexStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("verify index connection: %w", err)
	}
	log.Printf("connected to Pinecone: %d vectors in index", stats.TotalVectorCount)

	return &PineconeStore{client: client, index: index}, nil
}

func (p *PineconeStore) Upsert(
	ctx context.Context,
	documentID string,
	chunks []string,
	embeddings [][]float64,
) error {
	if len(chunks) != len(embeddings) {
		return fmt.Errorf(
			"chunk/embedding count mismatch: %d chunks, %d embeddings",
			len(chunks), len(embeddings),
		)
	}

	vectors := make([]*pinecone.Vector, 0, len(embeddings))

	for i := range embeddings {
		values := make([]float32, len(embeddings[i]))
		for j, v := range embeddings[i] {
			values[j] = float32(v)
		}

		metadata, err := structpb.NewStruct(map[string]interface{}{
			"text":        chunks[i],
			"document_id": documentID,
		})
		if err != nil {
			return fmt.Errorf("build metadata for chunk %d: %w", i, err)
		}

		vectors = append(vectors, &pinecone.Vector{
			Id:       fmt.Sprintf("%s-%d", documentID, i),
			Values:   &values, // *[]float32 in go-pinecone v3
			Metadata: metadata,
		})
	}

	count, err := p.index.UpsertVectors(ctx, vectors)
	if err != nil {
		return fmt.Errorf("upsert vectors: %w", err)
	}

	log.Printf("upserted %d vectors for document %s", count, documentID)
	return nil
}
