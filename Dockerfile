# ---- Stage 1: build the Go binary ----
    FROM golang:1.26 AS builder

    WORKDIR /app
    
    COPY go.mod go.sum ./
    RUN go mod download
    
    COPY . .
    
    RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server
    
    # ---- Stage 2: runtime image ----
    FROM debian:bookworm-slim
    
    WORKDIR /app
    
    # Install pdftotext (Poppler) + certs for HTTPS to S3/Pinecone/Voyage/Anthropic
    RUN apt-get update && \
        apt-get install -y --no-install-recommends poppler-utils ca-certificates && \
        rm -rf /var/lib/apt/lists/*
    
    COPY --from=builder /app/server /app/server
    
    EXPOSE 8080
    
    CMD ["/app/server"]