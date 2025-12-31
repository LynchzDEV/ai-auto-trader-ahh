# Build stage
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Install xx for cross-compilation and build dependencies
RUN apk add --no-cache clang lld
COPY --from=tonistiigi/xx / /

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Install target-specific build dependencies and build
RUN xx-apk add --no-cache gcc musl-dev
RUN CGO_ENABLED=1 GOOS=$(xx-info os) GOARCH=$(xx-info arch) \
    CC="xx-clang" \
    go build -ldflags="-w -s" -o server .

# Runtime stage
FROM alpine:3.20

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata sqlite

# Copy binary from builder
COPY --from=builder /app/server .

# Create data directory for SQLite
RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./server"]
