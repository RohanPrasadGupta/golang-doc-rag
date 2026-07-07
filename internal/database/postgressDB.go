package database

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
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
