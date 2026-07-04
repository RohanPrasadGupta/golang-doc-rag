package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// Storage now requires a context as the first param — Go convention:
// ctx is ALWAYS the first argument in any function that does I/O.
type Storage interface {
	Save(ctx context.Context, id string, data io.Reader) (string, error)
}

func NewServer(store Storage) *chi.Mux {
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

		id := uuid.New().String() // generate a unique id for the file

		// r.Context() is the request's context. If the client disconnects,
		// this context is cancelled and the S3 upload aborts automatically.

		path, err := store.Save(r.Context(), id, file) // save the file to the storage
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
