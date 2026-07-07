package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/RohanPrasadGupta/golang-doc-rag/internal/config"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/database"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/server"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/storage"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/vectordb"
)

func main() {
	config.LoadConfig()

	store, err := storage.NewS3Storage()

	if err != nil {
		log.Fatal("failed to create S3 storage:", err)
	}

	vectorStore, err := vectordb.NewPinecone(context.Background())
	if err != nil {
		log.Fatal("failed to connect to Pinecone: ", err)
	}

	postgresDB, err := database.NewPostgres(context.Background())
	if err != nil {
		log.Fatal("failed to connect to Postgres: ", err)
	}

	srv := server.NewServer(store, vectorStore, postgresDB)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, srv))

	// http.ListenAndServe(":"+port, srv)
}
