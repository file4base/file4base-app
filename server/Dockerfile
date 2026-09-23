# Multi-stage Dockerfile for File4Base Go Backend
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git and build dependencies
RUN apk add --no-cache git

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Minimal production image
FROM alpine:3.20

WORKDIR /app
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/server /app/server

EXPOSE 8080

ENTRYPOINT ["/app/server"]
