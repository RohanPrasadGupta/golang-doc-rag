package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/RohanPrasadGupta/golang-doc-rag/internal/chunk"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/embed"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/extract"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/vectordb"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type Storage interface {
	Save(ctx context.Context, id string, data io.Reader) (string, error)
}

func NewServer(store Storage, vectorStore *vectordb.PineconeStore) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/documents", func(w http.ResponseWriter, r *http.Request) {

		file, handler, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Failed to get file!",
			})
			return
		}

		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {

			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to read file!",
			})
			return
		}

		content, err := extract.ExtractText(data)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Failed to extract PDF text!",
			})
			return
		}

		log.Printf("extracted %d characters", len(content))

		chunks := chunk.SplitText(content, 1000, 200)

		embeddings, err := embed.EmbedTexts(r.Context(), chunks)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to embed chunks!",
			})
			return
		}

		log.Printf("embedded %d chunks", len(embeddings))

		id := uuid.New().String() // generate a unique id for the file

		if err := vectorStore.Upsert(r.Context(), id, chunks, embeddings); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to store vectors!",
			})
			return
		}

		path, err := store.Save(r.Context(), id, bytes.NewReader(data)) // save the file to the storage
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to save file!",
			})
			return
		}

		response := map[string]interface{}{
			"Status":  http.StatusOK,
			"Message": "File uploaded successfully!",
			"ID":      id,
			"File":    handler.Filename,
			"Size":    handler.Size,
			"Path":    path,
		}

		json.NewEncoder(w).Encode(response)
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{"Status": http.StatusOK, "Message": "Server is running!"}
		json.NewEncoder(w).Encode(response)
	})
	return r
}
