set dotenv-load := false

# List available recipes
default:
    @just --list

# Run all checks (unit tests + e2e + vet + fmt + gocyclo + staticcheck + deadcode).
# CI runs exactly this recipe — add new checks here, not in ci.yml.
check: test e2e vet fmt-check gocyclo staticcheck deadcode

# Run unit tests. e2e is separate (`just e2e`): it shells out to `go build`,
# so Go's test cache can't see source changes and would serve stale results.
test *args:
    go test ./cmd/... ./internal/... {{ args }}

# Run unit tests with verbose output
test-v *args:
    go test -v ./cmd/... ./internal/... {{ args }}

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

# Lint tools are pinned in go.mod (`tool` directive); bump with `go get -tool <pkg>@<version>`.

# Run cyclomatic-complexity check.
# Threshold 20 is a reasonable ceiling for non-test code.
gocyclo:
    go tool gocyclo -over 20 .

# Run staticcheck (bugs, simplifications, deprecations, unused code).
staticcheck:
    go tool staticcheck ./...

# Fail on functions unreachable from main. Test-only helpers belong in _test.go files.
# deadcode always exits 0, so fail on any output.
deadcode:
    #!/usr/bin/env bash
    set -euo pipefail
    out=$(go tool deadcode ./...)
    if [ -n "$out" ]; then
        echo "unreachable code (delete it, or move test-only helpers into _test.go):"
        echo "$out"
        exit 1
    fi

# Build the gw binary (version from git tag)
build:
    go build -ldflags "-X github.com/nicksenap/grove/cmd.Version=$(git describe --tags --always)" -o gw ./cmd/gw

# Run e2e tests against the built gw binary.
# Set GROVE_EXTERNAL_E2E=1 to include the opt-in public HTTPS clone check.
e2e: build
    GW_BIN="{{justfile_directory()}}/gw" go test ./e2e -count=1

# Set up dev environment (git hooks)
dev:
    git config core.hooksPath .githooks

# Tag a new release (usage: just release 0.13.0)
release version:
    #!/usr/bin/env bash
    set -euo pipefail
    git tag -a "v{{ version }}" -m "Release {{ version }}"
    git push origin "v{{ version }}"
