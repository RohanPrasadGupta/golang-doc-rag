package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/RohanPrasadGupta/golang-doc-rag/internal/chunk"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/claude"
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
type AskRequest struct {
	UserQuestion string `json:"question"`
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

	r.Post("/ask", func(w http.ResponseWriter, r *http.Request) {
		var req AskRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}

		if req.UserQuestion == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Question is required!",
			})
			return
		}

		fmt.Println("question:", req.UserQuestion)

		embeddedQuestion, err := embed.EmbedTexts(r.Context(), []string{req.UserQuestion})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to embed question!",
			})
			return
		}

		// fmt.Println("embededQuestion:", embededQuestion)

		matches, err := vectorStore.Query(r.Context(), embeddedQuestion[0], 5)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to fetch similar chunks!",
			})
			return
		}
		fmt.Println("matches:", matches)

		combinedMatchesText := ""
		for _, match := range matches {
			combinedMatchesText += match.Text + "\n"
		}

		claudeResponse, err := claude.Query(r.Context(), req.UserQuestion, combinedMatchesText)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to query Claude!",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":   http.StatusOK,
			"Question": req.UserQuestion,
			"Answer":   claudeResponse,
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{"Status": http.StatusOK, "Message": "Server is running!"}
		json.NewEncoder(w).Encode(response)
	})
	return r
}
