set dotenv-load := false

# List available recipes
default:
    @just --list

# Run all checks (test + vet + fmt + gocyclo + staticcheck)
check: test vet fmt-check gocyclo staticcheck

# Run tests
test *args:
    go test ./... {{ args }}

# Run tests with verbose output
test-v *args:
    go test -v ./... {{ args }}

# Run go vet
vet:
    go vet ./...

# Fail if any file needs reformatting. Fix with: gofmt -w .
fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out=$(gofmt -l .)
    if [ -n "$out" ]; then
        echo "files need gofmt:"
        echo "$out"
        exit 1
    fi

# Run cyclomatic-complexity check. Auto-installs gocyclo if missing.
# Threshold 20 is a reasonable ceiling for non-test code.
gocyclo:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v gocyclo >/dev/null 2>&1 || go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
    gocyclo -over 20 .

# Run staticcheck (bugs, simplifications, deprecations, unused code). Auto-installs if missing.
staticcheck:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@latest
    staticcheck ./...

# Build the gw binary (version from git tag)
build:
    go build -ldflags "-X github.com/nicksenap/grove/cmd.Version=$(git describe --tags --always)" -o gw ./cmd/gw

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
