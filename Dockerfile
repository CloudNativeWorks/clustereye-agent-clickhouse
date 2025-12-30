# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
ARG VERSION=1.0.0
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.version=${VERSION} -X main.buildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ') -X main.gitCommit=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')" \
    -o clustereye-agent-clickhouse \
    main.go

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create user for running the agent
RUN addgroup -g 1000 clustereye && \
    adduser -D -u 1000 -G clustereye clustereye

# Create directories
RUN mkdir -p /etc/clustereye /var/log/clustereye && \
    chown -R clustereye:clustereye /etc/clustereye /var/log/clustereye

# Copy binary from builder
COPY --from=builder /build/clustereye-agent-clickhouse /usr/local/bin/

# Copy example configuration
COPY agent.yml.example /etc/clustereye/agent.yml.example

# Set user
USER clustereye

# Set working directory
WORKDIR /home/clustereye

# Expose profiling port (optional)
EXPOSE 6060

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD pgrep -x clustereye-agent || exit 1

# Run the agent
ENTRYPOINT ["/usr/local/bin/clustereye-agent-clickhouse"]
CMD ["-config", "/etc/clustereye/agent.yml"]
