# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32

## Build stage
# Pinned to a digest for supply-chain safety; the readable tag documents the
# version. Renovate keeps both the tag and the digest up to date (see docs/ci.md).
FROM golang:1.26-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS builder

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
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates tzdata sqlite-libs

WORKDIR /app
COPY --from=builder /build/screenshotter /app/screenshotter

# Default config location; mount your config here
VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["/app/screenshotter"]
