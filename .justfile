# Run lint, check-format, and test (default target).
default: lint check-format test

# Run all tests.
test:
    go test ./...

# Run go vet and staticcheck.
lint:
    go vet ./...
    go tool staticcheck ./...

# Format all Go source files in place.
format:
    gofmt -w .

# Check formatting without modifying files; fails if any file is unformatted.
check-format:
    @test -z "$(gofmt -l .)" || \
        (echo "The following files are not formatted (run 'just format' to fix):" && \
         gofmt -l . && exit 1)

alias all := build

# Build the screenshotter binary.
build:
    go build -o screenshotter ./cmd/screenshotter

# Install the binary into $GOBIN (or $GOPATH/bin).
install:
    go install ./cmd/screenshotter

# Remove the installed binary from $GOBIN (or $GOPATH/bin).
uninstall:
    #!/usr/bin/env bash
    set -euo pipefail
    bin=$(go env GOBIN)
    [[ -z "$bin" ]] && bin="$(go env GOPATH)/bin"
    rm -f "$bin/screenshotter"
    echo "Removed $bin/screenshotter"

# Run the local binary with config.toml (builds first).
run: build check-config
    ./screenshotter -config config.toml

# Fail with a clear message if config.toml is missing.
[private]
check-config:
    @test -f config.toml || \
        (echo "error: config.toml not found — copy config.example.toml and edit it" >&2 && exit 1)
