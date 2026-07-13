package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

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
	"golang.org/x/sync/errgroup"
)

const (
	maxUploadBytes   = 10 << 20 // 10 MB
	ingestTimeout    = 5 * time.Minute
	uploadRateLimit  = 10
	uploadRateWindow = time.Minute
)

type Storage interface {
	Save(ctx context.Context, id string, data io.Reader, uploadType_ string) (string, error)
	AwsS3DeleteDocumt(ctx context.Context, s3Path string) error
}

type AskRequest struct {
	UserQuestion string `json:"question"`
	DocumentID   string `json:"document_id,omitempty"`
}

type CoverLetterRequest struct {
	ID             string `json:"id"`
	JobDescription string `json:"job_description"`
}

type NewResumeRequest struct {
	ID             string                 `json:"id"`
	UserUpdates    map[string]interface{} `json:"userUpdates"`
	JobDescription string                 `json:"job_description"`
}

type DeleteRequest struct {
	ID     string `json:"id"`
	S3Path string `json:"s3_path"`
}

type ScoreJDRequest struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

func NewServer(store Storage, vectorStore *vectordb.PineconeStore, postgresDB *database.PostgresStore) *chi.Mux {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "https://golang-doc-ai.netlify.app"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(2 * time.Minute))

	uploadLimiter := newIPRateLimiter(uploadRateLimit, uploadRateWindow)

	r.With(uploadLimiter.middleware, limitRequestBody(maxUploadBytes)).Post("/documents", func(w http.ResponseWriter, r *http.Request) {
		file, handler, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Failed to get file!",
			})
			return
		}
		defer file.Close()

		data, err := readUpload(file, maxUploadBytes)
		if err != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
				"Status":  http.StatusRequestEntityTooLarge,
				"Message": "File too large! Maximum upload size is 10MB.",
			})
			return
		}

		id := uuid.New().String()
		filename := handler.Filename
		size := int64(len(data))

		go processDocumentUpload(store, vectorStore, postgresDB, id, filename, data)

		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"Status":  http.StatusAccepted,
			"Message": "Upload accepted, processing in background.",
			"ID":      id,
			"File":    filename,
			"Size":    size,
		})
	})

	r.Get("/documents", func(w http.ResponseWriter, r *http.Request) {
		documents, err := postgresDB.ListDocuments(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to list documents!",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":    http.StatusOK,
			"Message":   "Documents listed successfully!",
			"Documents": documents,
		})
	})

	r.Delete("/documents", func(w http.ResponseWriter, r *http.Request) {
		var req DeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}

		if err := store.AwsS3DeleteDocumt(r.Context(), req.S3Path); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from S3!",
			})
			return
		}

		if err := postgresDB.DeleteDocument(r.Context(), req.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from database!",
			})
			return
		}

		if err := vectorStore.DeleteByDocumentIDPineCone(r.Context(), req.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from Pinecone!",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":   http.StatusOK,
			"Message":  "Document deleted successfully!",
			"Document": req.ID,
			"S3Path":   req.S3Path,
		})
	})

	r.Post("/ask", func(w http.ResponseWriter, r *http.Request) {
		var req AskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}

		if req.UserQuestion == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Question is required!",
			})
			return
		}

		embeddedQuestion, err := embed.EmbedTexts(r.Context(), []string{req.UserQuestion}, embed.InputTypeQuery)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to embed question!",
			})
			return
		}

		matches, err := vectorStore.Query(r.Context(), embeddedQuestion[0], 5, req.DocumentID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to fetch similar chunks!",
			})
			return
		}

		const minScore = 0.3

		relevant := make([]vectordb.Match, 0, len(matches))
		for _, m := range matches {
			if m.Score >= minScore {
				relevant = append(relevant, m)
			}
		}

		if len(relevant) == 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"Status":   http.StatusOK,
				"Question": req.UserQuestion,
				"Answer":   "I couldn't find anything relevant in the uploaded documents.",
			})
			return
		}

		var combined strings.Builder
		for _, match := range relevant {
			combined.WriteString(match.Text)
			combined.WriteByte('\n')
		}

		claudeResponse, err := claude.Query(r.Context(), req.UserQuestion, combined.String())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to query Claude!",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":   http.StatusOK,
			"Question": req.UserQuestion,
			"Answer":   claudeResponse,
		})
	})

	r.With(uploadLimiter.middleware, limitRequestBody(maxUploadBytes)).Post("/resumeAnalysis/upload", func(w http.ResponseWriter, r *http.Request) {
		file, handler, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Failed to get file!",
			})
			return
		}
		defer file.Close()

		data, err := readUpload(file, maxUploadBytes)
		if err != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
				"Status":  http.StatusRequestEntityTooLarge,
				"Message": "File too large! Maximum upload size is 10MB.",
			})
			return
		}

		id := uuid.New().String()
		filename := handler.Filename
		size := int64(len(data))

		go processResumeUpload(store, vectorStore, postgresDB, id, filename, data)

		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"Status":  http.StatusAccepted,
			"Message": "Resume upload accepted, processing in background.",
			"ID":      id,
			"File":    filename,
			"Size":    size,
		})
	})

	r.Get("/resumeAnalysis/get/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		analysis, err := postgresDB.GetResumeAnalysis(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get resume analysis!",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":   http.StatusOK,
			"Message":  "Resume analysis fetched successfully!",
			"Analysis": analysis,
		})
	})

	r.Get("/resumeAnalysis/getAll", func(w http.ResponseWriter, r *http.Request) {
		analyses, err := postgresDB.GetAllResumeAnalysis(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get all resume analyses!",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":   http.StatusOK,
			"Message":  "All resume analyses fetched successfully!",
			"Analyses": analyses,
		})
	})

	r.Delete("/resumeAnalysis/delete", func(w http.ResponseWriter, r *http.Request) {
		var req DeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}

		if err := store.AwsS3DeleteDocumt(r.Context(), req.S3Path); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from S3!",
			})
			return
		}

		if err := postgresDB.DeleteResumeAnalysis(r.Context(), req.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete resume analysis!",
			})
			return
		}

		if err := vectorStore.DeleteByDocumentIDPineCone(r.Context(), req.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete document from Pinecone!",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":   http.StatusOK,
			"Message":  "Resume data deleted successfully!",
			"Document": req.ID,
			"S3Path":   req.S3Path,
		})
	})

	r.Post("/resumeAnalysis/score_jd", func(w http.ResponseWriter, r *http.Request) {
		var req ScoreJDRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}
		if req.Content == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Content is required!",
			})
			return
		}
		if req.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "ID is required!",
			})
			return
		}

		var (
			jdReport        string
			userInformation database.ResumeAnalysis
		)

		g, gctx := errgroup.WithContext(r.Context())
		g.Go(func() error {
			var err error
			jdReport, err = claude.QueryJDExtraction(gctx, req.Content)
			return err
		})
		g.Go(func() error {
			var err error
			userInformation, err = postgresDB.GetResumeAnalysis(gctx, req.ID)
			return err
		})

		if err := g.Wait(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to prepare JD scoring!",
			})
			return
		}

		userInformationJSON, err := json.Marshal(userInformation)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user information!",
			})
			return
		}

		jdScoring, err := claude.QueryJDScoring(r.Context(), string(userInformationJSON), jdReport)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to score JD!",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":          http.StatusOK,
			"Message":         "JD scored successfully!",
			"jdReport":        jdReport,
			"JDScoring":       jdScoring,
			"userInformation": req.ID,
		})
	})

	r.Post("/resumeAnalysis/cover_letter", func(w http.ResponseWriter, r *http.Request) {
		var req CoverLetterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}

		if req.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "ID is required!",
			})
			return
		}

		if req.JobDescription == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Job description is required!",
			})
			return
		}

		userInformation, err := postgresDB.GetResumeAnalysis(r.Context(), req.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get user information!",
			})
			return
		}

		userInformationJSON, err := json.Marshal(userInformation)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user information!",
			})
			return
		}

		coverLetter, err := claude.QueryJOBCoverLetter(r.Context(), string(userInformationJSON), req.JobDescription)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to generate cover letter!",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":      http.StatusOK,
			"Message":     "Cover letter generated successfully!",
			"CoverLetter": coverLetter,
		})
	})

	r.Post("/resumeAnalysis/new_resume", func(w http.ResponseWriter, r *http.Request) {
		var req NewResumeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}
		if req.UserUpdates == nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "User updates are required!",
			})
			return
		}
		if req.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "ID is required!",
			})
			return
		}

		if req.JobDescription == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Job description is required!",
			})
			return
		}

		userInformation, err := postgresDB.GetResumeAnalysis(r.Context(), req.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get user information!",
			})
			return
		}

		userInformationJSON, err := json.Marshal(userInformation)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user information!",
			})
			return
		}

		userUpdatesJSON, err := json.MarshalIndent(req.UserUpdates, "", "  ")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user updates!",
			})
			return
		}

		resumeLatexCode, err := claude.QueryNewResume(r.Context(), string(userInformationJSON), string(userUpdatesJSON), req.JobDescription)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to generate new resume!",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"Status":          http.StatusOK,
			"Message":         "New resume generated successfully!",
			"ResumeLatexCode": resumeLatexCode,
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"Status": http.StatusOK, "Message": "Server is running!"})
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"Status": http.StatusOK, "Message": "Server is running!"})
	})

	return r
}

func processDocumentUpload(
	store Storage,
	vectorStore *vectordb.PineconeStore,
	postgresDB *database.PostgresStore,
	id, filename string,
	data []byte,
) {
	ctx, cancel := context.WithTimeout(context.Background(), ingestTimeout)
	defer cancel()

	content, err := extract.ExtractText(data)
	if err != nil {
		log.Printf("document %s: extract failed: %v", id, err)
		return
	}

	chunks := chunk.SplitText(content, 1000, 200)
	embeddings, err := embed.EmbedTexts(ctx, chunks, embed.InputTypeDocument)
	if err != nil {
		log.Printf("document %s: embed failed: %v", id, err)
		return
	}

	if err := vectorStore.Upsert(ctx, id, chunks, embeddings); err != nil {
		log.Printf("document %s: pinecone upsert failed: %v", id, err)
		return
	}

	path, err := store.Save(ctx, id, bytes.NewReader(data), "document")
	if err != nil {
		log.Printf("document %s: s3 save failed: %v", id, err)
		_ = vectorStore.DeleteByDocumentIDPineCone(ctx, id)
		return
	}

	if err := postgresDB.SaveDocument(ctx, id, filename, path, len(chunks)); err != nil {
		log.Printf("document %s: postgres save failed: %v", id, err)
		_ = store.AwsS3DeleteDocumt(ctx, path)
		_ = vectorStore.DeleteByDocumentIDPineCone(ctx, id)
		return
	}

	log.Printf("document %s: processing complete (%d chunks)", id, len(chunks))
}

func processResumeUpload(
	store Storage,
	vectorStore *vectordb.PineconeStore,
	postgresDB *database.PostgresStore,
	id, filename string,
	data []byte,
) {
	ctx, cancel := context.WithTimeout(context.Background(), ingestTimeout)
	defer cancel()

	content, err := extract.ExtractText(data)
	if err != nil {
		log.Printf("resume %s: extract failed: %v", id, err)
		return
	}

	chunks := chunk.SplitText(content, 1000, 200)

	var (
		embeddings          [][]float64
		extractedSkillsJSON string
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		embeddings, err = embed.EmbedTexts(gctx, chunks, embed.InputTypeDocument)
		return err
	})
	g.Go(func() error {
		var err error
		extractedSkillsJSON, err = claude.QueryResumeExtraction(gctx, content)
		return err
	})
	if err := g.Wait(); err != nil {
		log.Printf("resume %s: embed/extract failed: %v", id, err)
		return
	}

	var analysisDataJson database.ResumeAnalysis
	if err := json.Unmarshal([]byte(extractedSkillsJSON), &analysisDataJson); err != nil {
		log.Printf("resume %s: parse extracted skills failed: %v", id, err)
		return
	}

	if err := vectorStore.UpsertResumeAnalysis(ctx, id, chunks, embeddings); err != nil {
		log.Printf("resume %s: pinecone upsert failed: %v", id, err)
		return
	}

	path, err := store.Save(ctx, id, bytes.NewReader(data), "resume_analysis")
	if err != nil {
		log.Printf("resume %s: s3 save failed: %v", id, err)
		_ = vectorStore.DeleteByDocumentIDPineCone(ctx, id)
		return
	}

	if err := postgresDB.SaveResumeAnalysis(ctx, id, filename, path, len(chunks), analysisDataJson); err != nil {
		log.Printf("resume %s: postgres save failed: %v", id, err)
		_ = store.AwsS3DeleteDocumt(ctx, path)
		_ = vectorStore.DeleteByDocumentIDPineCone(ctx, id)
		return
	}

	log.Printf("resume %s: processing complete (%d chunks)", id, len(chunks))
}

func readUpload(r io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errUploadTooLarge
	}
	return data, nil
}

var errUploadTooLarge = &uploadTooLargeError{}

type uploadTooLargeError struct{}

func (e *uploadTooLargeError) Error() string { return "upload too large" }

func limitRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type ipRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
}

type visitor struct {
	count       int
	windowStart time.Time
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	rl := &ipRateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *ipRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.allow(ip) {
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"Status":  http.StatusTooManyRequests,
				"Message": "Rate limit exceeded. Try again later.",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, ok := rl.visitors[ip]
	if !ok || now.Sub(v.windowStart) >= rl.window {
		rl.visitors[ip] = &visitor{count: 1, windowStart: now}
		return true
	}
	if v.count >= rl.limit {
		return false
	}
	v.count++
	return true
}

func (rl *ipRateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, v := range rl.visitors {
			if now.Sub(v.windowStart) >= rl.window {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
