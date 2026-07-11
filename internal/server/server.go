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

		path, err := store.Save(r.Context(), id, bytes.NewReader(data), "document") // save the file to the storage
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

		const minScore = 0.3 // tune this — see note below

		relevant := make([]vectordb.Match, 0, len(matches))
		for _, m := range matches {
			if m.Score >= minScore {
				relevant = append(relevant, m)
			}
		}

		if len(relevant) == 0 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":   http.StatusOK,
				"Question": req.UserQuestion,
				"Answer":   "I couldn't find anything relevant in the uploaded documents.",
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

	r.Post("/resumeAnalysis/upload", func(w http.ResponseWriter, r *http.Request) {
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
				"Status":    http.StatusBadRequest,
				"Message":   "Failed to extract PDF text!",
				"File_Name": handler.Filename,
				"File_Size": handler.Size,
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

		extractedSkillsJSON, err := claude.QueryResumeExtraction(r.Context(), content)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to extract skills!",
			})
			return
		}

		var analysisDataJson database.ResumeAnalysis
		if err := json.Unmarshal([]byte(extractedSkillsJSON), &analysisDataJson); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to parse extracted skills!",
			})
			return
		}

		id := uuid.New().String()

		if err := vectorStore.UpsertResumeAnalysis(r.Context(), id, chunks, embeddings); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to store vectors!",
			})
			return
		}

		path, err := store.Save(r.Context(), id, bytes.NewReader(data), "resume_analysis") // save the file to the storage
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to save file!",
			})
			return
		}

		err = postgresDB.SaveResumeAnalysis(r.Context(), id, handler.Filename, path, len(chunks), analysisDataJson)
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
			"Message": "Resume analysis uploaded successfully!",
			"ID":      id,
			"File":    handler.Filename,
			"Size":    handler.Size,
			"Path":    path,
		}
		json.NewEncoder(w).Encode(response)
	})

	r.Get("/resumeAnalysis/get/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		analysis, err := postgresDB.GetResumeAnalysis(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get resume analysis!",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":   http.StatusOK,
			"Message":  "Resume analysis fetched successfully!",
			"Analysis": analysis,
		})
	})

	r.Get("/resumeAnalysis/getAll", func(w http.ResponseWriter, r *http.Request) {
		analyses, err := postgresDB.GetAllResumeAnalysis(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get all resume analyses!",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":   http.StatusOK,
			"Message":  "All resume analyses fetched successfully!",
			"Analyses": analyses,
		})
	})

	r.Delete("/resumeAnalysis/delete", func(w http.ResponseWriter, r *http.Request) {
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

		err = postgresDB.DeleteResumeAnalysis(r.Context(), req.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to delete resume analysis!",
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
			"Message":  "Resume data deleted successfully!",
			"Document": req.ID,
			"S3Path":   req.S3Path,
		})
	})

	r.Post("/resumeAnalysis/score_jd", func(w http.ResponseWriter, r *http.Request) {
		var req ScoreJDRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}
		if req.Content == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Content is required!",
			})
			return
		}
		if req.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "ID is required!",
			})
			return
		}

		jdReport, err := claude.QueryJDExtraction(r.Context(), req.Content)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to extract JD!",
			})
			return
		}

		userInformation, err := postgresDB.GetResumeAnalysis(r.Context(), req.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get user information!",
			})
			return
		}

		userInformationJSON, err := json.Marshal(userInformation)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user information!",
			})
			return
		}

		jdScoring, err := claude.QueryJDScoring(r.Context(), string(userInformationJSON), jdReport)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to score JD!",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":          http.StatusOK,
			"Message":         "JD scored successfully!",
			"jdReport":        jdReport,
			"JDScoring":       jdScoring,
			"userInformation": req.ID,
		})
	})

	r.Post("/resumeAnalysis/cover_letter", func(w http.ResponseWriter, r *http.Request) {
		var req CoverLetterRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}

		if req.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "ID is required!",
			})
			return
		}

		if req.JobDescription == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Job description is required!",
			})
			return
		}

		userInformation, err := postgresDB.GetResumeAnalysis(r.Context(), req.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get user information!",
			})
			return
		}

		userInformationJSON, err := json.Marshal(userInformation)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user information!",
			})
			return
		}

		coverLetter, err := claude.QueryJOBCoverLetter(r.Context(), string(userInformationJSON), string(req.JobDescription))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to generate cover letter!",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":      http.StatusOK,
			"Message":     "Cover letter generated successfully!",
			"CoverLetter": coverLetter,
		})
	})

	r.Post("/resumeAnalysis/new_resume", func(w http.ResponseWriter, r *http.Request) {
		var req NewResumeRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Invalid JSON body!",
			})
			return
		}
		if req.UserUpdates == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "User updates are required!",
			})
			return
		}
		if req.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "ID is required!",
			})
			return
		}

		if req.JobDescription == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusBadRequest,
				"Message": "Job description is required!",
			})
			return
		}

		userInformation, err := postgresDB.GetResumeAnalysis(r.Context(), req.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to get user information!",
			})
			return
		}

		userInformationJSON, err := json.Marshal(userInformation)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user information!",
			})
			return
		}

		userUpdatesJSON, err := json.MarshalIndent(req.UserUpdates, "", "  ")

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to marshal user updates!",
			})
			return
		}

		resumeLatexCode, err := claude.QueryNewResume(r.Context(), string(userInformationJSON), string(userUpdatesJSON), string(req.JobDescription))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Status":  http.StatusInternalServerError,
				"Message": "Failed to generate new resume!",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"Status":          http.StatusOK,
			"Message":         "New resume generated successfully!",
			"ResumeLatexCode": resumeLatexCode,
		})

	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{"Status": http.StatusOK, "Message": "Server is running!"}
		json.NewEncoder(w).Encode(response)
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{"Status": http.StatusOK, "Message": "Server is running!"}
		json.NewEncoder(w).Encode(response)
	})

	return r
}
