# Multi-stage Dockerfile for File4Base Go Backend
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install git and build dependencies
RUN apk add --no-cache git

# Cache go modules
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=auto
RUN go mod download

# Copy source code and build
COPY . .
ENV GOTOOLCHAIN=auto
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Minimal production image
FROM alpine:3.20

WORKDIR /app
RUN apk --no-cache add ca-certificates tzdata curl

COPY --from=builder /app/server /app/server

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/healthz/liveness || exit 1

ENTRYPOINT ["/app/server"]
