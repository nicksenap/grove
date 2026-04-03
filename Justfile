set dotenv-load := false

# List available recipes
default:
    @just --list

# Run all checks (test + vet)
check: test vet

# Run tests
test *args:
    go test ./... {{ args }}

# Run tests with verbose output
test-v *args:
    go test -v ./... {{ args }}

# Run go vet
vet:
    go vet ./...

# Build the gw binary
build:
    go build -o gw ./cmd/gw

# Run e2e tests
e2e: build
    bash e2e/run.sh

# Set up dev environment (git hooks)
dev:
    git config core.hooksPath .githooks

# Tag a new release (usage: just release 0.13.0)
release version:
    #!/usr/bin/env bash
    set -euo pipefail
    git tag -a "v{{ version }}" -m "Release {{ version }}"
    git push origin "v{{ version }}"
