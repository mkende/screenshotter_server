# syntax=docker/dockerfile:1

## Build stage
FROM golang:alpine AS builder

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
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata sqlite-libs

WORKDIR /app
COPY --from=builder /build/screenshotter /app/screenshotter

# Default config location; mount your config here
VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["/app/screenshotter"]
