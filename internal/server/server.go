package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/RohanPrasadGupta/golang-doc-rag/internal/chunk"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/claude"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/database"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/embed"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/extract"
	"github.com/RohanPrasadGupta/golang-doc-rag/internal/vectordb"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
)

type Storage interface {
	Save(ctx context.Context, id string, data io.Reader) (string, error)
	AwsS3DeleteDocumt(ctx context.Context, s3Path string) error
}
type AskRequest struct {
	UserQuestion string `json:"question"`
	DocumentID   string `json:"document_id,omitempty"`
}

type DeleteRequest struct {
	ID     string `json:"id"`
	S3Path string `json:"s3_path"`
}

func NewServer(store Storage, vectorStore *vectordb.PineconeStore, postgresDB *database.PostgresStore) *chi.Mux {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

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

		err = postgresDB.SaveDocument(r.Context(), id, handler.Filename, path, len(chunks))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to save document info!",
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

	r.Get("/documents", func(w http.ResponseWriter, r *http.Request) {
		documents, err := postgresDB.ListDocuments(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to list documents!",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":    http.StatusOK,
			"Message":   "Documents listed successfully!",
			"Documents": documents,
		})
	})

	r.Delete("/documents", func(w http.ResponseWriter, r *http.Request) {

		var req DeleteRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}

		err = store.AwsS3DeleteDocumt(r.Context(), req.S3Path)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from S3!",
			})
			return
		}

		err = postgresDB.DeleteDocument(r.Context(), req.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from database!",
			})
			return
		}

		err = vectorStore.DeleteByDocumentIDPineCone(r.Context(), req.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from Pinecone!",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":   http.StatusOK,
			"Message":  "Document deleted successfully!",
			"Document": req.ID,
			"S3Path":   req.S3Path,
		})
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

		matches, err := vectorStore.Query(r.Context(), embeddedQuestion[0], 5, req.DocumentID)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to fetch similar chunks!",
			})
			return
		}

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
