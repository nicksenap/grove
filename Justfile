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

# Build from this checkout and make it the active gw
gw-use-source:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v brew >/dev/null 2>&1 || { echo "error: Homebrew is required" >&2; exit 1; }
    brew list --versions grove >/dev/null 2>&1 || { echo "error: Homebrew grove is not installed" >&2; exit 1; }

    source_dir="${XDG_DATA_HOME:-$HOME/.local/share}/grove/source"
    source_bin="$source_dir/gw"
    active_bin="$(brew --prefix)/bin/gw"
    mkdir -p "$source_dir"
    go build -ldflags "-X github.com/nicksenap/grove/cmd.Version=$(git describe --tags --always)" -o "$source_bin.tmp" ./cmd/gw
    chmod 0755 "$source_bin.tmp"
    mv "$source_bin.tmp" "$source_bin"

    if [ -L "$active_bin" ] && [ "$(readlink "$active_bin")" = "$source_bin" ]; then
        : # Already using the source binary; the atomic build above updated it.
    elif [ -e "$active_bin" ] || [ -L "$active_bin" ]; then
        resolved="$(realpath "$active_bin")"
        case "$resolved" in
            "$(brew --cellar)/grove/"*)
                brew unlink grove >/dev/null
                ln -s "$source_bin" "$active_bin"
                ;;
            *)
                echo "error: refusing to replace unexpected $active_bin -> $resolved" >&2
                exit 1
                ;;
        esac
    else
        ln -s "$source_bin" "$active_bin"
    fi
    echo "Using source build: $source_bin"
    "$active_bin" --version

# Restore the Homebrew-managed gw binary
gw-use-brew:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v brew >/dev/null 2>&1 || { echo "error: Homebrew is required" >&2; exit 1; }
    brew list --versions grove >/dev/null 2>&1 || { echo "error: Homebrew grove is not installed" >&2; exit 1; }

    source_bin="${XDG_DATA_HOME:-$HOME/.local/share}/grove/source/gw"
    active_bin="$(brew --prefix)/bin/gw"
    if [ -L "$active_bin" ] && [ "$(readlink "$active_bin")" = "$source_bin" ]; then
        rm "$active_bin"
    elif [ -e "$active_bin" ] || [ -L "$active_bin" ]; then
        resolved="$(realpath "$active_bin")"
        case "$resolved" in
            "$(brew --cellar)/grove/"*) ;;
            *) echo "error: refusing to replace unexpected $active_bin -> $resolved" >&2; exit 1 ;;
        esac
    fi
    brew link grove >/dev/null
    echo "Using Homebrew build: $(realpath "$active_bin")"
    "$active_bin" --version

# Show which gw binary is active
gw-active:
    #!/usr/bin/env bash
    set -euo pipefail
    active_bin="$(command -v gw || true)"
    if [ -z "$active_bin" ]; then
        echo "gw is not on PATH"
        exit 1
    fi
    echo "Command:  $active_bin"
    echo "Binary:   $(realpath "$active_bin")"
    printf "Version:  "
    "$active_bin" --version

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
