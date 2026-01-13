# Build stage
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with pure Go (no CGO needed for modernc.org/sqlite)
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-w -s" -o server .

# Runtime stage
FROM alpine:3.20

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/server .

# Create data directory for SQLite
RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./server"]
