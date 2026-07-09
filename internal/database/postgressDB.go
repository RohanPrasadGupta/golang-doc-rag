package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

type Document struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	S3Path     string    `json:"s3_path"`
	ChunkCount int       `json:"chunk_count"`
	CreatedAt  time.Time `json:"created_at"`
}

func NewPostgres(ctx context.Context) (*PostgresStore, error) {
	databaseURL := os.Getenv("POSTGRES_DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("POSTGRES_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	err = pool.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("ping pool: %w", err)
	}
	log.Println("connected to Postgres")
	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) SaveDocument(ctx context.Context, id, filename, s3Path string, chunkCount int) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO documents (id, filename, s3_path, chunk_count) VALUES ($1, $2, $3, $4)`,
		id, filename, s3Path, chunkCount,
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (p *PostgresStore) ListDocuments(ctx context.Context) ([]Document, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, filename, s3_path, chunk_count, created_at
		 FROM documents ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	var documents []Document
	for rows.Next() {
		var doc Document
		err := rows.Scan(&doc.ID, &doc.Filename, &doc.S3Path, &doc.ChunkCount, &doc.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		documents = append(documents, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}

	return documents, nil
}

func (p *PostgresStore) DeleteDocument(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx,
		`DELETE FROM documents WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}
