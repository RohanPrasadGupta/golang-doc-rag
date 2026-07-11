# Document RAG & Resume ATS Backend (Go)

A Go HTTP backend that (1) turns uploaded PDFs into a searchable knowledge base via Voyage embeddings + Pinecone + Claude, and (2) analyzes resumes for ATS matching — score against job descriptions, generate cover letters, and produce tailored LaTeX resumes.

**Author:** Rohan Prasad Gupta  
**Language:** Go 1.25.6  
**Purpose:** Portfolio project demonstrating RAG pipelines, vector search, LLM workflows, and clean Go service design

---

## Table of Contents

- [Features](#features)
- [Tech Stack](#tech-stack)
- [Architecture](#architecture)
- [File Structure](#file-structure)
- [Environment Variables](#environment-variables)
- [Getting Started](#getting-started)
- [API Endpoints](#api-endpoints)
- [Database Schema](#database-schema)
- [Key Design Decisions](#key-design-decisions)
- [Roadmap](#roadmap)

---

## Features

### Document RAG

- **PDF upload & indexing** — extract text, chunk, embed, store vectors + metadata
- **Semantic Q&A** — ask over all documents or a single document via optional `document_id`
- **Voyage AI embeddings** — `voyage-4-lite`
- **Pinecone retrieval** — top-5 similarity search with optional document filter and 0.3 min score
- **Claude Haiku answers** — `claude-haiku-4-5`, grounded only in retrieved context
- **Three-store persistence** — PostgreSQL (metadata), AWS S3 (raw PDFs), Pinecone (vectors)

### Resume / ATS toolkit

- **Resume upload** — extract ATS keyword fields with Claude and persist them
- **JD scoring** — extract JD requirements, then score resume fit
- **Cover letter generation** — Claude-written letter from resume + JD
- **Tailored LaTeX resume** — rebuild resume with user updates + JD targeting
- **List / get / delete** resume analyses across S3, Postgres, and Pinecone

### Platform

- **CORS-ready** — `http://localhost:5173` and `https://golang-doc-ai.netlify.app`
- **Docker** — Compose for Postgres; multi-stage image with `poppler-utils`

---

## Tech Stack

| Layer | Technology | Purpose |
|---|---|---|
| Language | Go 1.25.6 | Compiled HTTP service |
| HTTP Router | Chi (`go-chi/chi/v5`) | Routing + middleware |
| CORS | `go-chi/cors` | Frontend origin allowlist |
| Database | PostgreSQL 17 + `pgx/v5` | Document + resume metadata |
| Object Storage | AWS S3 (`aws-sdk-go-v2`) | Raw PDF storage |
| Vector DB | Pinecone (`go-pinecone/v3`) | Chunk embeddings + similarity search |
| Embeddings | Voyage AI REST API | `voyage-4-lite` vectors |
| LLM | Anthropic Claude SDK | `claude-haiku-4-5` for RAG + resume flows |
| PDF Extraction | Poppler `pdftotext` | PDF → plain text |
| Env Loading | godotenv | Optional `.env` for local development |
| Container | Docker + Compose | App image + local Postgres |

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  Client (Browser / API Consumer)                                 │
│  localhost:5173  ·  golang-doc-ai.netlify.app                    │
└───────────────────────────────┬──────────────────────────────────┘
                                │  HTTP
                                ↓
┌──────────────────────────────────────────────────────────────────┐
│  Chi HTTP Server (PORT, default 8080)                            │
│                                                                  │
│  Document RAG:                                                   │
│    GET    /  ·  GET /health                                      │
│    POST   /documents          upload + index PDF                 │
│    GET    /documents          list metadata                      │
│    DELETE /documents          S3 + Postgres + Pinecone           │
│    POST   /ask                RAG Q&A                            │
│                                                                  │
│  Resume ATS:                                                     │
│    POST   /resumeAnalysis/upload                                 │
│    GET    /resumeAnalysis/get/{id}                               │
│    GET    /resumeAnalysis/getAll                                 │
│    DELETE /resumeAnalysis/delete                                 │
│    POST   /resumeAnalysis/score_jd                               │
│    POST   /resumeAnalysis/cover_letter                           │
│    POST   /resumeAnalysis/new_resume                             │
└──────┬──────────────┬──────────────┬──────────────┬──────────────┘
       ↓              ↓              ↓              ↓
┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────────────┐
│ PostgreSQL │ │  AWS S3    │ │  Pinecone  │ │ Voyage AI + Claude │
│ documents  │ │ documents/ │ │ chunk      │ │ Haiku 4.5          │
│ resume_    │ │ resume_    │ │ vectors    │ │                    │
│ analysis   │ │ analysis/  │ │            │ │                    │
└────────────┘ └────────────┘ └────────────┘ └────────────────────┘
```

### Document upload pipeline (`POST /documents`)

```
multipart file "file"
        ↓
  pdftotext (Poppler)
        ↓
  chunk.SplitText(size=1000, overlap=200)
        ↓
  Voyage embed chunks (voyage-4-lite)
        ↓
  Pinecone Upsert  →  vectors: {document_id}-{index}
        ↓
  S3 PutObject     →  key: documents/{uuid}
        ↓
  Postgres INSERT  →  documents table
```

### Ask pipeline (`POST /ask`)

```
{"question": "...", "document_id": "optional"}
        ↓
  Embed question (Voyage)
        ↓
  Pinecone Query (topK=5, optional document_id filter)
        ↓
  Keep matches with score >= 0.3
        ↓
  Claude Haiku (context = retrieved chunk text)
        ↓
  {"Status", "Question", "Answer"}
```

### Resume analysis pipeline (`POST /resumeAnalysis/upload`)

```
multipart file "file"
        ↓
  pdftotext → chunk → embed (same as documents)
        ↓
  Claude QueryResumeExtraction → ATS keyword JSON
        ↓
  Pinecone Upsert + S3 (resume_analysis/{uuid}) + Postgres INSERT
```

### JD score / cover letter / new resume

```
score_jd:      JD text → Claude JD extract → Claude score vs resume ATS fields
cover_letter:  resume ATS + JD → Claude cover letter
new_resume:    resume ATS + userUpdates + JD → Claude LaTeX resume
```

All Claude resume prompts live in `internal/claude/claueSystem.go`.

---

## File Structure

```
golang-doc-rag/
├── cmd/
│   └── server/
│       └── main.go                 # Entry: config, S3, Pinecone, Postgres, HTTP listen
├── internal/
│   ├── config/
│   │   └── config.go               # Optional godotenv.Load() for local .env
│   ├── server/
│   │   └── server.go               # Chi router + all HTTP handlers
│   ├── database/
│   │   └── postgressDB.go          # pgx pool; documents + resume_analysis CRUD
│   ├── storage/
│   │   ├── s3.go                   # AWS S3 upload / delete (wired in main)
│   │   └── local.go                # Local filesystem storage (not wired)
│   ├── extract/
│   │   └── extract.go              # PDF → text via pdftotext
│   ├── chunk/
│   │   └── chunk.go                # Rune-based sliding-window chunking
│   ├── embed/
│   │   └── embed.go                # Voyage AI embeddings client
│   ├── vectordb/
│   │   └── pinecone.go             # Pinecone upsert / query / delete-by-filter
│   ├── claude/
│   │   ├── claudeQuery.go          # Anthropic Messages API wrappers
│   │   └── claueSystem.go          # System prompts (extract, JD, score, letter, LaTeX)
│   └── models/
│       └── resume.go               # ResumeAnalysis struct (unused duplicate)
├── docker-compose.yml              # Local PostgreSQL 17
├── Dockerfile                      # Multi-stage build + poppler-utils
├── go.mod / go.sum
├── .env                            # Local secrets (never commit)
├── .gitignore
└── README.md
```

---

## Environment Variables

Create a `.env` in the project root for local development. In production (e.g. Render), inject the same variables on the host — a missing `.env` file is fine.

```env
# Server
PORT=8080

# PostgreSQL
POSTGRES_DATABASE_URL=postgres://postgres:postgres@localhost:5432/docrag?sslmode=disable

# AWS S3
AWS_REGION=ap-southeast-1
AWS_BUCKET=your-bucket-name
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key

# Pinecone
PINECONE_API_KEY=pcsk_...
PINECONE_HOST=https://your-index-xxxx.svc.region.pinecone.io

# Voyage AI
VOYAGE_API_KEY=pa-...

# Anthropic
ANTHROPIC_API_KEY=sk-ant-...
```

| Variable | Required | Purpose |
|---|---|---|
| `PORT` | No (default `8080`) | HTTP listen port |
| `POSTGRES_DATABASE_URL` | Yes | Postgres connection string |
| `AWS_REGION` | Yes | S3 region |
| `AWS_BUCKET` | Yes | S3 bucket name |
| `AWS_ACCESS_KEY_ID` | Yes* | AWS credentials (*or IAM role) |
| `AWS_SECRET_ACCESS_KEY` | Yes* | AWS credentials (*or IAM role) |
| `PINECONE_API_KEY` | Yes | Pinecone auth |
| `PINECONE_HOST` | Yes | Pinecone index host URL |
| `VOYAGE_API_KEY` | Yes | Voyage embeddings API |
| `ANTHROPIC_API_KEY` | Yes | Claude API |

**Pinecone index:** Create an index whose dimension matches `voyage-4-lite` before starting the server.

---

## Getting Started

### Prerequisites

- Go 1.25+ (matches `go.mod`)
- Docker Desktop (for PostgreSQL)
- Poppler (`pdftotext`) on your PATH for local runs  
  - macOS: `brew install poppler`  
  - Linux: `apt install poppler-utils`
- Accounts / resources: AWS S3 bucket, Pinecone index, Voyage AI key, Anthropic key

### Setup

**1. Clone the repository**

```bash
git clone https://github.com/RohanPrasadGupta/golang-doc-rag.git
cd golang-doc-rag
```

**2. Install Go dependencies**

```bash
go mod download
```

**3. Start PostgreSQL**

```bash
docker compose up -d
docker compose ps
```

**4. Create tables** (no auto-migration in the app)

```bash
psql "postgres://postgres:postgres@localhost:5432/docrag?sslmode=disable" <<'SQL'
CREATE TABLE IF NOT EXISTS documents (
    id          TEXT PRIMARY KEY,
    filename    TEXT NOT NULL,
    s3_path     TEXT NOT NULL,
    chunk_count INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS resume_analysis (
    id                            TEXT PRIMARY KEY,
    filename                      TEXT NOT NULL,
    s3_path                       TEXT NOT NULL,
    chunk_count                   INTEGER NOT NULL DEFAULT 0,
    skills                        TEXT[] NOT NULL DEFAULT '{}',
    experience_keywords           TEXT[] NOT NULL DEFAULT '{}',
    job_titles                    TEXT[] NOT NULL DEFAULT '{}',
    project_keywords              TEXT[] NOT NULL DEFAULT '{}',
    education_keywords            TEXT[] NOT NULL DEFAULT '{}',
    certifications                TEXT[] NOT NULL DEFAULT '{}',
    domain_keywords               TEXT[] NOT NULL DEFAULT '{}',
    soft_skills                   TEXT[] NOT NULL DEFAULT '{}',
    action_verbs                  TEXT[] NOT NULL DEFAULT '{}',
    quantified_achievements       TEXT[] NOT NULL DEFAULT '{}',
    explicit_years_of_experience  TEXT[] NOT NULL DEFAULT '{}'
);
SQL
```

**5. Create your `.env`** (see [Environment Variables](#environment-variables))

**6. Run the server**

```bash
go run ./cmd/server
```

Expected logs:

```
connected to Pinecone: N vectors in index
connected to Postgres
server starting on port 8080
```

**7. Verify health**

```bash
curl http://localhost:8080/health
```

```json
{
  "Status": 200,
  "Message": "Server is running!"
}
```

### Run with Docker

```bash
docker build -t golang-doc-rag .
docker run -p 8080:8080 --env-file .env golang-doc-rag
```

The image installs `poppler-utils` so PDF extraction works inside the container. Postgres still runs via `docker compose` (or a managed instance).

---

## API Endpoints

All routes are root-level (no `/api` prefix). Envelope keys use PascalCase (`Status`, `Message`). Nested DB fields use snake_case (`s3_path`, `chunk_count`, …).

---

### Health

#### `GET /` · `GET /health`

```bash
curl http://localhost:8080/health
```

```json
{
  "Status": 200,
  "Message": "Server is running!"
}
```

---

### Document RAG

#### 1. Upload a Document — `POST /documents`

Upload a PDF. The server extracts text, chunks it (1000 runes, 200 overlap), embeds with Voyage, upserts to Pinecone, saves to S3 (`documents/{id}`), and inserts Postgres metadata.

```bash
curl -X POST http://localhost:8080/documents \
  -F "file=@/path/to/report.pdf"
```

**Response `200`:**

```json
{
  "Status": 200,
  "Message": "File uploaded successfully!",
  "ID": "550e8400-e29b-41d4-a716-446655440000",
  "File": "report.pdf",
  "Size": 123456,
  "Path": "documents/550e8400-e29b-41d4-a716-446655440000"
}
```

| Status | Message |
|---|---|
| 400 | `Failed to get file!` |
| 400 | `Failed to extract PDF text!` |
| 500 | `Failed to read file!` / `Failed to embed chunks!` / `Failed to store vectors!` / `Failed to save file!` / `Failed to save document info!` |

---

#### 2. List Documents — `GET /documents`

```bash
curl http://localhost:8080/documents
```

```json
{
  "Status": 200,
  "Message": "Documents listed successfully!",
  "Documents": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "filename": "report.pdf",
      "s3_path": "documents/550e8400-e29b-41d4-a716-446655440000",
      "chunk_count": 42,
      "created_at": "2026-07-10T09:00:00Z"
    }
  ]
}
```

---

#### 3. Delete a Document — `DELETE /documents`

Removes S3 object → Postgres row → Pinecone vectors (by `document_id` filter).

```bash
curl -X DELETE http://localhost:8080/documents \
  -H "Content-Type: application/json" \
  -d '{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "s3_path": "documents/550e8400-e29b-41d4-a716-446655440000"
  }'
```

```json
{
  "Status": 200,
  "Message": "Document deleted successfully!",
  "Document": "550e8400-e29b-41d4-a716-446655440000",
  "S3Path": "documents/550e8400-e29b-41d4-a716-446655440000"
}
```

---

#### 4. Ask a Question — `POST /ask`

Embeds the question, retrieves top 5 similar chunks (optionally scoped to one document), and asks Claude to answer from that context only.

```bash
# All documents
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{"question": "What is the main conclusion of the report?"}'

# Single document
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{
    "question": "What is the main conclusion of the report?",
    "document_id": "550e8400-e29b-41d4-a716-446655440000"
  }'
```

**Answer found:**

```json
{
  "Status": 200,
  "Question": "What is the main conclusion of the report?",
  "Answer": "Based on the uploaded documents, the main conclusion is..."
}
```

**Nothing relevant (score &lt; 0.3):**

```json
{
  "Status": 200,
  "Question": "What is the main conclusion of the report?",
  "Answer": "I couldn't find anything relevant in the uploaded documents."
}
```

| Status | Message |
|---|---|
| 400 | `Invalid JSON body!` / `Question is required!` |
| 500 | `Failed to embed question!` / `Failed to fetch similar chunks!` / `Failed to query Claude!` |

---

### Resume ATS

#### 5. Upload Resume — `POST /resumeAnalysis/upload`

Extracts PDF text, chunks/embeds for Pinecone, runs Claude ATS keyword extraction, stores PDF at `resume_analysis/{id}`, and saves ATS fields in Postgres.

```bash
curl -X POST http://localhost:8080/resumeAnalysis/upload \
  -F "file=@/path/to/resume.pdf"
```

```json
{
  "Status": 200,
  "Message": "Resume analysis uploaded successfully!",
  "ID": "550e8400-e29b-41d4-a716-446655440000",
  "File": "resume.pdf",
  "Size": 45678,
  "Path": "resume_analysis/550e8400-e29b-41d4-a716-446655440000"
}
```

| Status | Message |
|---|---|
| 400 | `Failed to get file!` / `Failed to extract PDF text!` |
| 500 | `Failed to embed chunks!` / `Failed to extract skills!` / `Failed to parse extracted skills!` / `Failed to store vectors!` / `Failed to save file!` / `Failed to save resume analysis!` |

---

#### 6. Get One Analysis — `GET /resumeAnalysis/get/{id}`

Returns ATS keyword fields only (no `id` / `filename` / `s3_path` / `chunk_count`).

```bash
curl http://localhost:8080/resumeAnalysis/get/550e8400-e29b-41d4-a716-446655440000
```

```json
{
  "Status": 200,
  "Message": "Resume analysis fetched successfully!",
  "Analysis": {
    "skills": ["Go", "React", "PostgreSQL"],
    "experience_keywords": ["backend", "microservices"],
    "job_titles": ["Software Engineer"],
    "project_keywords": ["RAG", "API"],
    "education_keywords": ["Computer Science"],
    "certifications": [],
    "domain_keywords": ["fintech"],
    "soft_skills": ["collaboration"],
    "action_verbs": ["built", "led"],
    "quantified_achievements": ["reduced latency by 40%"],
    "explicit_years_of_experience": ["3 years"]
  }
}
```

---

#### 7. List All Analyses — `GET /resumeAnalysis/getAll`

```bash
curl http://localhost:8080/resumeAnalysis/getAll
```

```json
{
  "Status": 200,
  "Message": "All resume analyses fetched successfully!",
  "Analyses": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "filename": "resume.pdf",
      "s3_path": "resume_analysis/550e8400-e29b-41d4-a716-446655440000",
      "chunk_count": 12,
      "skills": ["Go", "React"],
      "experience_keywords": ["backend"],
      "job_titles": ["Software Engineer"],
      "project_keywords": ["RAG"],
      "education_keywords": ["Computer Science"],
      "certifications": [],
      "domain_keywords": ["fintech"],
      "soft_skills": ["collaboration"],
      "action_verbs": ["built"],
      "quantified_achievements": ["reduced latency by 40%"],
      "explicit_years_of_experience": ["3 years"]
    }
  ]
}
```

---

#### 8. Delete Resume Analysis — `DELETE /resumeAnalysis/delete`

```bash
curl -X DELETE http://localhost:8080/resumeAnalysis/delete \
  -H "Content-Type: application/json" \
  -d '{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "s3_path": "resume_analysis/550e8400-e29b-41d4-a716-446655440000"
  }'
```

```json
{
  "Status": 200,
  "Message": "Resume data deleted successfully!",
  "Document": "550e8400-e29b-41d4-a716-446655440000",
  "S3Path": "resume_analysis/550e8400-e29b-41d4-a716-446655440000"
}
```

---

#### 9. Score Resume vs Job Description — `POST /resumeAnalysis/score_jd`

Extracts structured JD requirements with Claude, then scores the stored resume ATS profile against them. `jdReport` and `JDScoring` are returned as raw Claude strings (prompted to be JSON).

```bash
curl -X POST http://localhost:8080/resumeAnalysis/score_jd \
  -H "Content-Type: application/json" \
  -d '{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "content": "We are hiring a Senior Go Engineer with experience in PostgreSQL, AWS, and distributed systems..."
  }'
```

```json
{
  "Status": 200,
  "Message": "JD scored successfully!",
  "jdReport": "{ ... Claude JD extraction JSON ... }",
  "JDScoring": "{ ... Claude scoring JSON ... }",
  "userInformation": "550e8400-e29b-41d4-a716-446655440000"
}
```

| Status | Message |
|---|---|
| 400 | `Invalid JSON body!` / `Content is required!` / `ID is required!` |
| 500 | `Failed to extract JD!` / `Failed to get user information!` / `Failed to marshal user information!` / `Failed to score JD!` |

---

#### 10. Generate Cover Letter — `POST /resumeAnalysis/cover_letter`

```bash
curl -X POST http://localhost:8080/resumeAnalysis/cover_letter \
  -H "Content-Type: application/json" \
  -d '{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "job_description": "Senior Go Engineer role at Acme..."
  }'
```

```json
{
  "Status": 200,
  "Message": "Cover letter generated successfully!",
  "CoverLetter": "{ ... Claude cover letter output ... }"
}
```

| Status | Message |
|---|---|
| 400 | `Invalid JSON body!` / `ID is required!` / `Job description is required!` |
| 500 | `Failed to get user information!` / `Failed to marshal user information!` / `Failed to generate cover letter!` |

---

#### 11. Generate Tailored LaTeX Resume — `POST /resumeAnalysis/new_resume`

Builds a new LaTeX resume from stored ATS fields, optional user updates, and the target JD.

```bash
curl -X POST http://localhost:8080/resumeAnalysis/new_resume \
  -H "Content-Type: application/json" \
  -d '{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "userUpdates": {
      "additional_information": "Open to remote roles",
      "additional_skills": ["Kubernetes"],
      "selected_missing_skills": ["Terraform"]
    },
    "job_description": "Senior Go Engineer role at Acme..."
  }'
```

```json
{
  "Status": 200,
  "Message": "New resume generated successfully!",
  "ResumeLatexCode": "\\documentclass{article}\n..."
}
```

| Status | Message |
|---|---|
| 400 | `Invalid JSON body!` / `User updates are required!` / `ID is required!` / `Job description is required!` |
| 500 | `Failed to get user information!` / `Failed to marshal user information!` / `Failed to marshal user updates!` / `Failed to generate new resume!` |

---

## Database Schema

### `documents` table

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT` | Primary key (UUID string) |
| `filename` | `TEXT` | Original upload filename |
| `s3_path` | `TEXT` | e.g. `documents/{id}` |
| `chunk_count` | `INTEGER` | Number of chunks / vectors |
| `created_at` | `TIMESTAMPTZ` | Default `NOW()` |

### `resume_analysis` table

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT` | Primary key (UUID string) |
| `filename` | `TEXT` | Original resume filename |
| `s3_path` | `TEXT` | e.g. `resume_analysis/{id}` |
| `chunk_count` | `INTEGER` | Number of chunks / vectors |
| `skills` | `TEXT[]` | Extracted hard skills |
| `experience_keywords` | `TEXT[]` | Experience-related keywords |
| `job_titles` | `TEXT[]` | Past / current titles |
| `project_keywords` | `TEXT[]` | Project keywords |
| `education_keywords` | `TEXT[]` | Education keywords |
| `certifications` | `TEXT[]` | Certifications |
| `domain_keywords` | `TEXT[]` | Domain / industry keywords |
| `soft_skills` | `TEXT[]` | Soft skills |
| `action_verbs` | `TEXT[]` | Action verbs used |
| `quantified_achievements` | `TEXT[]` | Metrics / quantified wins |
| `explicit_years_of_experience` | `TEXT[]` | Explicit YoE mentions |

### Pinecone vectors

| Field | Value |
|---|---|
| Vector ID | `{document_id}-{chunk_index}` |
| Metadata `text` | Chunk content |
| Metadata `document_id` | Parent UUID (documents and resumes share the same index namespace) |

### S3 layout

| Key | Content |
|---|---|
| `documents/{uuid}` | Raw uploaded document PDF |
| `resume_analysis/{uuid}` | Raw uploaded resume PDF |

---

## Key Design Decisions

### Why Chi?

Thin, idiomatic router on `net/http`. Logger, recoverer, and CORS compose without a heavy framework.

### Why three stores?

- **Postgres** — listable metadata for the UI  
- **S3** — durable original files  
- **Pinecone** — similarity search over chunk embeddings  

### Why Poppler `pdftotext`?

Reliable CLI extraction via stdin/stdout. The Docker image installs `poppler-utils` so production does not depend on a Go PDF library.

### Why chunk size 1000 / overlap 200?

Rune-based sliding windows keep chunks model-friendly while overlap preserves context across boundaries (step = 800).

### Why Voyage + Claude?

Voyage specializes in embeddings; Claude Haiku is fast and cheap for grounded Q&A and structured resume workflows. RAG answers are constrained to retrieved context only.

### Why separate resume ATS fields in Postgres?

JD scoring, cover letters, and LaTeX generation need structured keyword arrays — not just raw PDF text. Claude extraction runs once on upload; later endpoints reuse those fields.

### Why system prompts in `claueSystem.go`?

Large, versionable prompts for extraction, JD parsing, scoring, cover letters, and LaTeX building stay out of handlers and are easy to iterate on.

### Why optional `document_id` on `/ask`?

Supports both “search everything” and “chat with this PDF” without separate endpoints.

---

## Roadmap

### Completed

- PDF document upload → extract → chunk → embed → Pinecone
- S3 + Postgres document metadata; list / delete
- RAG `/ask` with optional document scoping
- Resume upload with Claude ATS extraction
- JD scoring, cover letter, and LaTeX resume generation
- Docker image with Poppler + Compose Postgres
- CORS for local Vite and Netlify frontend

### Upcoming

- Database migrations on startup (or SQL migration files)
- Auth / API keys for upload and delete
- Async upload pipeline for large PDFs
- `.env.example` and dependency health checks
- Support for additional file types (TXT, DOCX)
- Rate limiting and upload size limits
- Query resume vectors in Pinecone for resume-side RAG

---

## License

MIT — free to use, modify, and distribute.

---

## Contact

**Rohan Prasad Gupta**  
Portfolio: [rohanpdgupta-portfolio.netlify.app](https://rohanpdgupta-portfolio.netlify.app)  
GitHub: [@RohanPrasadGupta](https://github.com/RohanPrasadGupta)  
Frontend: [golang-doc-ai.netlify.app](https://golang-doc-ai.netlify.app)
