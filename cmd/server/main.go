package main

import (
	"net/http"
	"os"

	"github.com/RohanPrasadGupta/golang-doc-rag/internal/config"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/server"
)

func main() {
	config.LoadConfig()
	server := server.NewServer()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.ListenAndServe(":"+port, server)
}
