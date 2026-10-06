# Multi-stage Dockerfile for File4Base Go Backend
# The builder runs on the build host's platform and cross-compiles for the
# target platform, so multi-arch builds (amd64 + arm64) need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS=linux
ARG TARGETARCH
# "v7" for linux/arm/v7 (32-bit Raspberry Pi OS); empty on amd64/arm64
ARG TARGETVARIANT

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
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} go build -ldflags="-w -s" -o /app/server ./cmd/server
# License notices of every Go module linked into the binary (and of Go itself)
RUN sh tools/third_party_notices.sh /app/server /app/THIRD_PARTY_NOTICES

# Minimal production image
FROM alpine:3.20
LABEL org.opencontainers.image.documentation="Third-party license notices: /usr/share/doc/file4base/THIRD_PARTY_NOTICES"

WORKDIR /app
RUN apk --no-cache add ca-certificates tzdata curl

COPY --from=builder /app/server /app/server
COPY --from=builder /app/THIRD_PARTY_NOTICES /usr/share/doc/file4base/THIRD_PARTY_NOTICES

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/healthz/liveness || exit 1

ENTRYPOINT ["/app/server"]
