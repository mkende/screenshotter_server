# syntax=docker/dockerfile:1

## Build stage
# Pinned to a digest for supply-chain safety; the readable tag documents the
# version. Renovate keeps both the tag and the digest up to date (see docs/ci.md).
FROM golang:1.26-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS builder

WORKDIR /build

# mattn/go-sqlite3 requires CGO; install gcc and SQLite headers
RUN apk add --no-cache gcc musl-dev sqlite-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build \
    -ldflags="-s -w -X github.com/mkende/screenshotter/server/internal/version.Version=${VERSION}" \
    -o screenshotter ./cmd/screenshotter

## Runtime stage
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

RUN apk add --no-cache ca-certificates tzdata sqlite-libs

WORKDIR /app
COPY --from=builder /build/screenshotter /app/screenshotter

# Default config location; mount your config here
VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["/app/screenshotter"]
