# Document RAG Backend (Go)

A Go backend that turns uploaded PDFs into a searchable knowledge base. Documents are extracted, chunked, embedded with Voyage AI, stored in Pinecone, and answered with Anthropic Claude — with metadata in PostgreSQL and original files in AWS S3.

**Author:** Rohan Prasad Gupta  
**Language:** Go 1.25.6  
**Purpose:** Portfolio project demonstrating RAG pipelines, vector search, cloud storage, and clean Go service design

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

- **PDF upload & indexing** — multipart upload extracts text, chunks it, embeds it, and stores vectors + metadata
- **RAG Q&A** — ask questions over all documents or a single document via optional `document_id`
- **Voyage AI embeddings** — `voyage-4-lite` for document and query vectors
- **Pinecone vector search** — top-5 similarity retrieval with optional document filter and 0.3 minimum score
- **Claude Haiku answers** — `claude-haiku-4-5` answers strictly from retrieved context
- **Three-store persistence** — PostgreSQL (metadata), AWS S3 (raw PDFs), Pinecone (vectors)
- **Document list & delete** — list uploaded docs; delete cleans S3, Postgres, and Pinecone
- **CORS-ready** — allows local Vite (`localhost:5173`) and the Netlify frontend

---

## Tech Stack

| Layer | Technology | Purpose |
|---|---|---|
| Language | Go 1.25.6 | Compiled HTTP service |
| HTTP Router | Chi (`go-chi/chi/v5`) | Lightweight routing + middleware |
| CORS | `go-chi/cors` | Frontend origin allowlist |
| Database | PostgreSQL 17 + `pgx/v5` | Document metadata |
| Object Storage | AWS S3 (`aws-sdk-go-v2`) | Raw PDF storage |
| Vector DB | Pinecone (`go-pinecone/v3`) | Chunk embeddings + similarity search |
| Embeddings | Voyage AI REST API | `voyage-4-lite` vectors |
| LLM | Anthropic Claude SDK | `claude-haiku-4-5` answers |
| PDF Extraction | Poppler `pdftotext` | PDF → plain text |
| Env Loading | godotenv | Optional `.env` for local development |
| Container | Docker + Compose | App image + local Postgres |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Client (Browser / API Consumer)                            │
│  localhost:5173  ·  golang-doc-ai.netlify.app               │
└────────────────────────────┬────────────────────────────────┘
                             │  HTTP
                             ↓
┌─────────────────────────────────────────────────────────────┐
│  Chi HTTP Server (PORT, default 8080)                       │
│                                                             │
│  Routes:                                                    │
│    GET    /health                                           │
│    POST   /documents     (upload + index PDF)               │
│    GET    /documents     (list metadata)                    │
│    DELETE /documents     (S3 + Postgres + Pinecone)         │
│    POST   /ask           (RAG question answering)           │
└──────┬──────────────┬──────────────┬──────────────┬─────────┘
       │              │              │              │
       ↓              ↓              ↓              ↓
┌────────────┐ ┌────────────┐ ┌────────────┐ ┌──────────────┐
│ PostgreSQL │ │  AWS S3    │ │  Pinecone  │ │ Voyage AI +  │
│ documents  │ │ documents/ │ │ chunk      │ │ Claude Haiku │
│ metadata   │ │ {uuid}     │ │ vectors    │ │              │
└────────────┘ └────────────┘ └────────────┘ └──────────────┘
```

### Upload pipeline (`POST /documents`)

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
        ↓
  JSON response (ID, File, Size, Path)
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
│   │   └── postgressDB.go          # pgx pool, documents CRUD
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
│   └── claude/
│       └── claudeQuery.go          # Anthropic Messages API for RAG answers
├── docker-compose.yml              # Local PostgreSQL 17
├── Dockerfile                      # Multi-stage build + poppler-utils
├── go.mod / go.sum
├── .env                            # Local secrets (never commit)
├── .gitignore
└── README.md
```

---

## Environment Variables

Create a `.env` in the project root for local development. In production (e.g. Render), inject the same variables in the host — a missing `.env` file is fine.

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

**4. Create the `documents` table** (no auto-migration in the app)

```bash
psql "postgres://postgres:postgres@localhost:5432/docrag?sslmode=disable" <<'SQL'
CREATE TABLE IF NOT EXISTS documents (
    id          TEXT PRIMARY KEY,
    filename    TEXT NOT NULL,
    s3_path     TEXT NOT NULL,
    chunk_count INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
SQL
```

**5. Create your `.env`** (see [Environment Variables](#environment-variables))

**6. Run the server**

```bash
go run ./cmd/server
```

You should see logs similar to:

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

All routes are root-level (no `/api` prefix). Success/error JSON often uses PascalCase keys (`Status`, `Message`).

### 1. Health Check

**Request:**

```bash
curl http://localhost:8080/health
```

**Response `200`:**

```json
{
  "Status": 200,
  "Message": "Server is running!"
}
```

---

### 2. Upload a Document

Upload a PDF. The server extracts text, chunks it (1000 runes, 200 overlap), embeds with Voyage, upserts to Pinecone, saves the file to S3, and inserts metadata into Postgres.

**Request:**

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

**Errors:**

| Status | Message |
|---|---|
| 400 | `Failed to get file!` |
| 400 | `Failed to extract PDF text!` |
| 500 | `Failed to read file!` |
| 500 | `Failed to embed chunks!` |
| 500 | `Failed to store vectors!` |
| 500 | `Failed to save file!` |
| 500 | `Failed to save document info!` |

---

### 3. List Documents

**Request:**

```bash
curl http://localhost:8080/documents
```

**Response `200`:**

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

**Error `500`:** `Failed to list documents!`

---

### 4. Delete a Document

Removes the object from S3, the row from Postgres, then vectors from Pinecone (by `document_id` metadata filter).

**Request:**

```bash
curl -X DELETE http://localhost:8080/documents \
  -H "Content-Type: application/json" \
  -d '{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "s3_path": "documents/550e8400-e29b-41d4-a716-446655440000"
  }'
```

**Response `200`:**

```json
{
  "Status": 200,
  "Message": "Document deleted successfully!",
  "Document": "550e8400-e29b-41d4-a716-446655440000",
  "S3Path": "documents/550e8400-e29b-41d4-a716-446655440000"
}
```

**Errors:**

| Status | Message |
|---|---|
| 400 | `Invalid JSON body!` |
| 500 | `Failed to delete document from S3!` |
| 500 | `Failed to delete document from database!` |
| 500 | `Failed to delete document from Pinecone!` |

---

### 5. Ask a Question (RAG)

Embeds the question, retrieves the top 5 similar chunks from Pinecone (optionally scoped to one document), and asks Claude to answer using that context only.

**Request (all documents):**

```bash
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{"question": "What is the main conclusion of the report?"}'
```

**Request (single document):**

```bash
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{
    "question": "What is the main conclusion of the report?",
    "document_id": "550e8400-e29b-41d4-a716-446655440000"
  }'
```

**Response `200` (answer found):**

```json
{
  "Status": 200,
  "Question": "What is the main conclusion of the report?",
  "Answer": "Based on the uploaded documents, the main conclusion is..."
}
```

**Response `200` (nothing relevant, score &lt; 0.3):**

```json
{
  "Status": 200,
  "Question": "What is the main conclusion of the report?",
  "Answer": "I couldn't find anything relevant in the uploaded documents."
}
```

**Errors:**

| Status | Message |
|---|---|
| 400 | `Invalid JSON body!` |
| 400 | `Question is required!` |
| 500 | `Failed to embed question!` |
| 500 | `Failed to fetch similar chunks!` |
| 500 | `Failed to query Claude!` |

---

## Database Schema

### `documents` table (PostgreSQL)

| Column | Type | Notes |
|---|---|---|
| `id` | `TEXT` | Primary key (UUID string from Go) |
| `filename` | `TEXT` | Original upload filename |
| `s3_path` | `TEXT` | S3 key, e.g. `documents/{id}` |
| `chunk_count` | `INTEGER` | Number of chunks / vectors upserted |
| `created_at` | `TIMESTAMPTZ` | Default `NOW()` |

Go struct (`internal/database/postgressDB.go`):

```go
type Document struct {
    ID         string    `json:"id"`
    Filename   string    `json:"filename"`
    S3Path     string    `json:"s3_path"`
    ChunkCount int       `json:"chunk_count"`
    CreatedAt  time.Time `json:"created_at"`
}
```

### Pinecone vectors

| Field | Value |
|---|---|
| Vector ID | `{document_id}-{chunk_index}` |
| Metadata `text` | Chunk content |
| Metadata `document_id` | Parent document UUID |

### S3 layout

| Key | Content |
|---|---|
| `documents/{uuid}` | Raw uploaded PDF bytes |

---

## Key Design Decisions

### Why Chi instead of a heavier framework?

Chi is a thin, idiomatic router on top of `net/http`. Middleware (logger, recoverer, CORS) composes cleanly without pulling in an ORM or DI framework.

### Why three stores (Postgres + S3 + Pinecone)?

Each store owns one concern:

- **Postgres** — listable document metadata for the UI
- **S3** — durable original files
- **Pinecone** — similarity search over chunk embeddings

### Why Poppler `pdftotext`?

Reliable CLI extraction with stdin/stdout (`pdftotext - -`). The Docker image installs `poppler-utils` so production does not depend on a Go PDF library.

### Why chunk size 1000 / overlap 200?

Rune-based sliding windows keep chunks roughly model-friendly while overlap preserves context across boundaries. Step size is `800` runes (`1000 - 200`).

### Why Voyage + Claude?

Voyage specializes in embeddings; Claude Haiku is fast and cheap for grounded Q&A. The system prompt forces answers from retrieved context only — no outside knowledge.

### Why optional `document_id` on `/ask`?

Supports both “search everything” and “chat with this PDF” UX without separate endpoints. Pinecone applies a metadata filter when `document_id` is set.

### Why score threshold 0.3?

Low-similarity matches are treated as irrelevant so Claude is not asked to invent answers from weak retrieval. Tunable in `server.go`.

---

## Roadmap

### Completed

- PDF upload → extract → chunk → embed → Pinecone upsert
- S3 storage + Postgres document metadata
- List / delete documents across all three stores
- RAG `/ask` with optional document scoping
- Docker image with Poppler + Compose Postgres
- CORS for local Vite and Netlify frontend

### Upcoming

- Database migrations on startup (or SQL migration files)
- Auth / API keys for upload and delete
- Async upload pipeline for large PDFs
- `.env.example` and health checks for dependencies
- Support for additional file types (TXT, DOCX)
- Rate limiting and upload size limits

---

## License

MIT — free to use, modify, and distribute.

---

## Contact

**Rohan Prasad Gupta**  
Portfolio: [rohanpdgupta-portfolio.netlify.app](https://rohanpdgupta-portfolio.netlify.app)  
GitHub: [@RohanPrasadGupta](https://github.com/RohanPrasadGupta)  
Frontend: [golang-doc-ai.netlify.app](https://golang-doc-ai.netlify.app)
