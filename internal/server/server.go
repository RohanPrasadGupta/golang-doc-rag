package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewServer() *chi.Mux {
	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Server is running!"))
	})
	return r
}
