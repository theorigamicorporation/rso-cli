# Developer entry point. CI runs the same recipes.
# Toolchain versions are pinned in .tool-versions (run `asdf install`).

set shell := ["bash", "-euo", "pipefail", "-c"]

binary := "rso"
version := `git describe --tags --always --dirty 2>/dev/null || echo dev`

# Show the available recipes
default:
    @just --list

# Build the binary into ./bin
build:
    go build -trimpath -ldflags "-s -w -X main.version={{version}}" -o bin/{{binary}} ./cmd/rso

# Install the binary into $GOBIN
install:
    go install -trimpath -ldflags "-s -w -X main.version={{version}}" ./cmd/rso

# Format Go code
fmt:
    go fmt ./...

# Run golangci-lint
lint:
    golangci-lint run ./...

# Run the tests (they talk to an in-memory fake API, never to the platform)
test:
    go test -race -count=1 ./...

# Run the tests without the race detector, for runners without a C toolchain
test-ci:
    go test -count=1 ./...

# Rewrite the golden files after an intended change to what a command prints
golden:
    go test ./internal/cli/ -count=1 -update

# Coverage over internal/..., as a summary and coverage.html
cover:
    go test -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | tail -n 1
    go tool cover -html=coverage.out -o coverage.html

# Check that go.mod and go.sum are tidy
tidy-check:
    go mod tidy
    git diff --exit-code -- go.mod go.sum

# Build release archives locally without publishing anything
snapshot:
    goreleaser release --snapshot --clean

# Everything CI checks
check: lint test tidy-check
