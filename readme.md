# Golang Document RAG Service

A production-shaped **Retrieval-Augmented Generation (RAG)** service written entirely in **Go**. Upload a PDF, and the service extracts its text, splits it into chunks, embeds them, stores them in a vector database, and lets you ask natural-language questions that are answered by Claude using *only* the content of your documents.

The whole pipeline is built from scratch — file storage, PDF text extraction, chunking, embeddings, vector search, metadata tracking, and LLM answering — wired together behind clean interfaces and dependency injection.

---

## What this project demonstrates

- Building a real backend service in Go with idiomatic project structure
- Integrating **four external systems** (AWS S3, Voyage AI, Pinecone, PostgreSQL) plus the **Anthropic API**
- The full RAG loop: **ingest → embed → retrieve → augment → generate**
- Hand-rolling an HTTP client for a third-party API (Voyage, which has no Go SDK)
- Using an official Go SDK where one exists (Pinecone) — knowing when to build vs. adopt
- Writing raw SQL against Postgres with `pgx` (no ORM)
- Vector-database **metadata filtering** to scope retrieval to a specific document
- Dependency injection, the interface pattern, context propagation, and safe error handling

---

## Architecture

The service has two core flows: **ingestion** (when a document is uploaded) and **query** (when a question is asked).

### Ingestion flow

```
   PDF upload
       │
       ▼
   Go API  ──────────────►  AWS S3        (store the raw PDF)
       │
       ├──►  pdftotext      (extract plain text from the PDF)
       │
       ├──►  Chunker        (split text into overlapping chunks)
       │
       ├──►  Voyage AI      (embed each chunk into a 1024-dim vector)
       │
       ├──►  Pinecone       (store vectors + metadata: text, document_id)
       │
       └──►  PostgreSQL     (record document metadata: id, filename, path, chunk count)
```

### Query flow

```
   Question (+ optional document_id)
       │
       ▼
   Go API
       │
       ├──►  Voyage AI      (embed the question)
       │
       ├──►  Pinecone       (find top-K nearest chunks; optionally filtered by document_id)
       │
       ├──►  Build prompt   (retrieved chunks as context + the question)
       │
       └──►  Claude         (answer using ONLY the provided context)
       │
       ▼
   JSON answer
```

### Why two AI providers?

Anthropic's API does **generation** (answering), not **embeddings**. Embeddings — turning text into vectors — are handled by **Voyage AI** (Anthropic's recommended embedding provider). So the stack deliberately splits responsibilities:

- **Voyage AI** → embeds text into vectors (for retrieval)
- **Claude (Anthropic)** → generates the final answer (from retrieved context)

The single generated **UUID** for each document ties all three storage systems together: the row in Postgres, the object in S3 (`documents/<uuid>`), and the vectors in Pinecone (`<uuid>-0`, `<uuid>-1`, …).

---

## Tech stack

| Layer                | Technology                              | Purpose                                          |
| -------------------- | --------------------------------------- | ------------------------------------------------ |
| Language             | Go                                      | API server and all orchestration logic           |
| HTTP router          | `go-chi/chi`                            | Lightweight, idiomatic routing + middleware      |
| Raw file storage     | AWS S3                                  | Stores the original uploaded PDFs                |
| Text extraction      | `pdftotext` (Poppler) via `os/exec`     | Extracts plain text from PDFs                    |
| Embeddings           | Voyage AI (`voyage-4-lite`)             | Turns text chunks into 1024-dim vectors          |
| Vector database      | Pinecone                                | Stores vectors, performs similarity search       |
| Metadata store       | PostgreSQL (via `pgx`)                  | Tracks which documents exist                     |
| LLM                  | Anthropic Claude (`claude-haiku-4-5`)   | Answers questions from retrieved context         |

---

## Project structure

```
golang-doc-rag/
├── cmd/
│   └── server/
│       └── main.go              # entry point — wires up all dependencies, starts the server
├── internal/
│   ├── config/
│   │   └── config.go            # loads environment variables from .env
│   ├── server/
│   │   └── server.go            # chi router + all HTTP handlers
│   ├── storage/
│   │   └── s3.go                # S3Storage — saves raw PDFs (implements the Storage interface)
│   ├── extract/
│   │   └── extract.go           # ExtractText — shells out to pdftotext
│   ├── chunk/
│   │   └── chunk.go             # SplitText — rune-safe overlapping chunking
│   ├── embed/
│   │   └── embed.go             # EmbedTexts — hand-rolled Voyage AI HTTP client
│   ├── vectordb/
│   │   └── pinecone.go          # PineconeStore — Upsert + Query (with metadata filtering)
│   ├── database/
│   │   └── postgres.go          # PostgresStore — SaveDocument + ListDocuments
│   └── claude/
│       └── claudeQuery.go       # Query — calls the Anthropic API to generate answers
├── docker-compose.yml           # local PostgreSQL
├── .env                         # your secrets (NOT committed)
├── .env.example                 # documents which env vars are required
├── go.mod
└── go.sum
```

The `internal/` directory uses Go's enforced privacy: packages under `internal/` can only be imported within this module, keeping the codebase cleanly separated.

---

## How it works, step by step

### 1. Upload (`POST /documents`)

1. The handler reads the uploaded file from the multipart form.
2. The raw bytes are read once into memory and reused (so both S3 and extraction get the same content without re-reading the stream).
3. **Text extraction** — the bytes are written to a temp file and `pdftotext` is invoked via `os/exec` to pull out plain text. (`pdftotext` is far more robust than pure-Go PDF libraries, which often return empty text on real-world PDFs.)
4. **Chunking** — the text is split into overlapping chunks (1000 characters wide, 200-character overlap). Overlap prevents facts near a chunk boundary from being split and lost. Chunking operates on `[]rune`, not raw bytes, so multi-byte UTF-8 characters are never cut in half.
5. **Embedding** — all chunks are sent to Voyage AI in one batched call, returning one 1024-dim vector per chunk.
6. **Vector storage** — each vector is upserted into Pinecone with a unique ID (`<uuid>-<index>`) and metadata (`text`, `document_id`).
7. **S3 storage** — the raw PDF is saved to S3 under `documents/<uuid>`.
8. **Metadata** — a row is inserted into Postgres recording the document.

### 2. Ask (`POST /ask`)

1. The question is embedded via Voyage.
2. Pinecone is queried for the top-5 most similar chunks. If a `document_id` is provided, a **metadata filter** scopes the search to that single document.
3. The retrieved chunk texts are pulled from the vectors' metadata and combined into a context block.
4. Claude is called with a system prompt instructing it to answer using **only** the provided context (an anti-hallucination guardrail), and the answer is returned.

### 3. List (`GET /documents`)

Returns all uploaded documents from Postgres, newest first.

---

## API reference

### `GET /health`

Health check.

**Response**
```json
{ "Status": 200, "Message": "Server is running!" }
```

### `POST /documents`

Upload a PDF. Body is `multipart/form-data` with a field named `file`.

**Example (curl)**
```bash
curl -F "file=@/path/to/document.pdf" http://localhost:8080/documents
```

**Response**
```json
{
  "Status": 200,
  "Message": "File uploaded successfully!",
  "ID": "7ee9e3fd-3950-4004-ae08-58fe6ba3a638",
  "File": "document.pdf",
  "Size": 157593,
  "Path": "documents/7ee9e3fd-3950-4004-ae08-58fe6ba3a638"
}
```

### `GET /documents`

List all uploaded documents.

**Response**
```json
{
  "Status": 200,
  "Message": "Documents listed successfully!",
  "Documents": [
    {
      "id": "7ee9e3fd-3950-4004-ae08-58fe6ba3a638",
      "filename": "document.pdf",
      "s3_path": "documents/7ee9e3fd-3950-4004-ae08-58fe6ba3a638",
      "chunk_count": 12,
      "created_at": "2026-07-08T00:16:41.476403+07:00"
    }
  ]
}
```

### `POST /ask`

Ask a question. Body is JSON.

| Field         | Type   | Required | Description                                            |
| ------------- | ------ | -------- | ------------------------------------------------------ |
| `question`    | string | yes      | The natural-language question                          |
| `document_id` | string | no       | If set, retrieval is scoped to this one document only  |

**Example — ask across all documents**
```bash
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{"question": "What are the AWS certifications?"}'
```

**Example — ask scoped to one document**
```bash
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{
    "question": "Where did they study?",
    "document_id": "7ee9e3fd-3950-4004-ae08-58fe6ba3a638"
  }'
```

**Response**
```json
{
  "Status": 200,
  "Question": "Where did they study?",
  "Answer": "Based on the retrieved document, they studied at ..."
}
```

---

## Getting started

### Prerequisites

- **Go** 1.22+ ([install](https://go.dev/dl/))
- **Docker** (for local PostgreSQL) ([install](https://docs.docker.com/get-docker/))
- **Poppler** (provides the `pdftotext` binary)
  - macOS: `brew install poppler`
  - Ubuntu/Debian: `sudo apt-get install poppler-utils`
- Accounts / keys for:
  - **AWS** — an S3 bucket + credentials
  - **Voyage AI** — a free API key ([voyageai.com](https://www.voyageai.com/)); the free tier is generous
  - **Pinecone** — a free account + a serverless index ([pinecone.io](https://www.pinecone.io/))
  - **Anthropic** — an API key ([console.anthropic.com](https://console.anthropic.com/))

### 1. Clone the repository

```bash
git clone https://github.com/RohanPrasadGupta/golang-doc-rag.git
cd golang-doc-rag
```

### 2. Install Go dependencies

```bash
go mod tidy
```

### 3. Create the Pinecone index

In the Pinecone console, create a **serverless** index with:

- **Dimension:** `1024` (must match the `voyage-4-lite` embedding size)
- **Metric:** `cosine`
- **Cloud/region:** any (e.g. AWS `us-east-1`)

Copy the index **host URL** (looks like `https://<index>-<id>.svc.<region>.pinecone.io`) — you'll need it below.

### 4. Set up environment variables

Copy the example file and fill in your values:

```bash
cp .env.example .env
```

Your `.env` should contain:

```env
# Server
PORT=8080

# AWS S3
AWS_REGION=us-east-1
AWS_BUCKET=your-bucket-name
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key

# Voyage AI (embeddings)
VOYAGE_API_KEY=your-voyage-key

# Pinecone (vector database)
PINECONE_API_KEY=your-pinecone-key
PINECONE_HOST=https://your-index-host.pinecone.io

# Anthropic (Claude)
ANTHROPIC_API_KEY=your-anthropic-key

# PostgreSQL
POSTGRES_DATABASE_URL=postgres://postgres:postgres@localhost:5432/docrag
```

> **Note:** AWS credentials can also be supplied via the standard AWS mechanisms (`~/.aws/credentials`, IAM roles, etc.) — the code uses `config.LoadDefaultConfig`, which picks them up automatically.

### 5. Start PostgreSQL

```bash
docker compose up -d
```

Wait until it reports healthy:

```bash
docker compose ps    # look for "healthy"
```

### 6. Create the database schema

Connect to the running database and create the `documents` table:

```bash
docker exec -it rag-postgres psql -U postgres -d docrag
```

Then run:

```sql
CREATE TABLE documents (
    id          UUID PRIMARY KEY,
    filename    TEXT NOT NULL,
    s3_path     TEXT NOT NULL,
    chunk_count INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Type `\q` to exit.

### 7. Run the server

```bash
go run cmd/server/main.go
```

On a successful start you should see something like:

```
connected to Pinecone: 0 vectors in index
connected to Postgres
server starting on port 8080
```

### 8. Try it out

```bash
# Upload a PDF
curl -F "file=@resume.pdf" http://localhost:8080/documents

# List documents
curl http://localhost:8080/documents

# Ask a question (use an ID from the list above to scope it)
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{"question": "Summarize this document."}'
```

---

## Docker Compose (PostgreSQL)

The included `docker-compose.yml` runs a local Postgres with a persistent volume:

```yaml
services:
  postgres:
    image: postgres:17
    container_name: rag-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: docrag
    ports:
      - "5432:5432"
    volumes:
      - rag-pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d docrag"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  rag-pgdata:
```

Useful commands:

```bash
docker compose up -d       # start
docker compose ps          # status
docker compose logs -f     # logs
docker compose down        # stop (data survives in the volume)
docker compose down -v     # stop AND wipe the data volume
```

---

## Design decisions & notes

- **`pdftotext` over pure-Go PDF libraries.** Pure-Go extractors frequently return empty text on real-world PDFs (non-standard font encodings). Shelling out to the battle-tested `pdftotext` handles nearly any PDF. Arguments are passed to `os/exec` separately (never concatenated into a shell string), so filenames with spaces or special characters can't cause injection.
- **Rune-based chunking.** Go strings are byte sequences, so slicing by byte index can split a multi-byte UTF-8 character in half. Chunking on `[]rune` avoids this.
- **Interface for storage.** `S3Storage` implements a `Storage` interface, so the storage backend could be swapped (e.g. for local disk or a mock in tests) without changing the handler code.
- **Hand-rolled Voyage client, official Pinecone SDK.** Voyage has no Go SDK, so its client is built directly with `net/http` and JSON structs. Pinecone has an official SDK, which is used instead of reimplementing its API — demonstrating judgment about when to build vs. adopt.
- **Context propagation.** The request's `context.Context` is threaded through I/O calls, so a client disconnect can cancel in-flight work.
- **Anti-hallucination prompt.** Claude is instructed to answer using only the retrieved context and to say so when the answer isn't present — which is what makes this a true RAG system rather than a general chatbot.

### Known limitations / future work

- **Distributed consistency.** Uploads write to S3, Pinecone, and Postgres in sequence with no shared transaction. A failure partway through can leave orphaned data (e.g. vectors with no Postgres record). A production system would address this with compensating cleanup or an outbox pattern.
- **No authentication** on the endpoints.
- **Schema is created manually**; a proper migration tool would be the next step.
- **No streaming** of Claude responses yet (answers return all at once).

---

## License

MIT (or your preferred license).
