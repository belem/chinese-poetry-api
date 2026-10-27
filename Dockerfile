# Stage 1: Build & Data Processing
FROM golang:1.27-alpine AS builder

# Install make, gcc, and musl-dev (required to compile SQLite C-bindings during make build)
RUN apk add --no-cache make gcc musl-dev

WORKDIR /build

# Copy your source code (including your git clone'd poetry submodules)
COPY . .

# Process the poetry text files into a solid, embedded SQLite database file
RUN make process-data

# Compile the Go application natively
RUN CGO_ENABLED=1 go build -tags sqlite_fts5 -ldflags "-s -w" -trimpath -o server ./cmd/server

# Stage 2: Runtime Environment
FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl

WORKDIR /app

# Create the data directory
RUN mkdir -p data
# Copy the compiled binary, config file, and the freshly generated local database files from Stage 1
COPY --from=builder /build/server .
COPY --from=builder /build/config.yaml .
COPY --from=builder /build/data/ ./data/

# Environment variables matching original defaults
ENV PORT=1279 \
    GIN_MODE=release \
    RATE_LIMIT_ENABLED=true \
    RATE_LIMIT_RPS=10 \
    RATE_LIMIT_BURST=20

EXPOSE 1279

# Direct execution bypasses startup.sh since data is already embedded in ./data
CMD ["./server"]
