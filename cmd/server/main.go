package main

import (
	"log"
	"net/http"
	"os"

	"github.com/RohanPrasadGupta/golang-doc-rag/internal/config"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/server"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/storage"
)

func main() {
	config.LoadConfig()

	store, err := storage.NewS3Storage()

	if err != nil {
		log.Fatal("failed to create S3 storage:", err)
	}

	srv := server.NewServer(store)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// log.Fatal wraps ListenAndServe so a startup failure (e.g. port in use)
	// is actually reported instead of silently exiting.
	log.Printf("server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, srv))

	// http.ListenAndServe(":"+port, srv)
}
