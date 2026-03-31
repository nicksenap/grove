set dotenv-load := false

grove_bin := env("HOME") / ".grove/bin"
go_dir    := justfile_directory() / "go"

# List available recipes
default:
    @just --list

# Run all checks (lint + format + tests)
check: lint fmt-check test

# Run ruff linter
lint:
    uv run ruff check src/ tests/

# Auto-fix lint errors
fix:
    uv run ruff check --fix src/ tests/

# Check formatting
fmt-check:
    uv run ruff format --check src/ tests/

# Auto-format code
fmt:
    uv run ruff format src/ tests/

# Run tests
test *args:
    uv run pytest tests/ {{ args }}

# Run tests with verbose output
test-v *args:
    uv run pytest tests/ -v {{ args }}

# Set up dev environment (editable install + git hooks)
dev:
    uv pip install -e .
    git config core.hooksPath .githooks

# ---------------------------------------------------------------------------
# Switch active gw implementation (go | python | brew)
# ---------------------------------------------------------------------------

# Show which gw is currently active
which:
    #!/usr/bin/env bash
    shim="{{ grove_bin }}/gw"
    if [ -L "$shim" ]; then
        target=$(readlink "$shim")
        echo "shim: $shim -> $target"
        if [[ "$target" == *"/go/"* ]]; then echo "impl: go"
        elif [[ "$target" == *"uv"* ]] || [[ "$target" == *"python"* ]]; then echo "impl: python (uv)"
        else echo "impl: unknown"; fi
    elif [ -f "$shim" ]; then
        echo "shim: $shim (direct binary)"
    else
        echo "shim: none (using PATH fallback)"
    fi
    # Check what `command gw` actually resolves to
    actual=$(PATH="{{ grove_bin }}:$PATH" command -v gw 2>/dev/null || true)
    echo "resolves: $actual"
    echo ""
    PATH="{{ grove_bin }}:$PATH" command gw --version 2>/dev/null || echo "(gw not runnable)"
    # Warn if shim dir not on PATH
    if [[ ":$PATH:" != *":{{ grove_bin }}:"* ]]; then
        echo ""
        echo "⚠ {{ grove_bin }} is not on your PATH."
        echo "  Add to ~/.zshrc:  export PATH=\"\$HOME/.grove/bin:\$PATH\""
    fi

# Switch to Go build
use-go: _ensure-shim-dir (_path-check)
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building Go binary..."
    cd "{{ go_dir }}" && go build -o gw .
    ln -sf "{{ go_dir }}/gw" "{{ grove_bin }}/gw"
    echo "✓ Switched to Go: {{ go_dir }}/gw"
    "{{ grove_bin }}/gw" --version

# Switch to Python editable install (uv)
use-python: _ensure-shim-dir (_path-check)
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Installing Python editable..."
    uv tool install --editable . --force --reinstall 2>&1 | tail -1
    py_gw=$(uv tool dir)/grove/bin/gw
    if [ ! -f "$py_gw" ]; then
        # fallback: find it
        py_gw=$(find "$(uv tool dir)" -name gw -type f 2>/dev/null | head -1)
    fi
    if [ -z "$py_gw" ]; then
        echo "Error: could not locate uv-installed gw"
        exit 1
    fi
    ln -sf "$py_gw" "{{ grove_bin }}/gw"
    echo "Switched to Python: $py_gw"
    "{{ grove_bin }}/gw" --version

# Switch to Homebrew version (removes shim, falls through to PATH)
use-brew: _ensure-shim-dir
    #!/usr/bin/env bash
    rm -f "{{ grove_bin }}/gw"
    brew_gw=$(brew --prefix 2>/dev/null)/bin/gw
    if [ -x "$brew_gw" ]; then
        echo "Switched to Homebrew: $brew_gw"
        "$brew_gw" --version
    else
        echo "Homebrew gw not installed. Run: brew install grove"
    fi

# Run Go e2e tests
e2e-go:
    cd "{{ go_dir }}" && go build -o gw . && bash e2e/run.sh

# Ensure ~/.grove/bin exists
_ensure-shim-dir:
    @mkdir -p "{{ grove_bin }}"

# Warn once if shim dir not on PATH
_path-check:
    #!/usr/bin/env bash
    if [[ ":$PATH:" != *":{{ grove_bin }}:"* ]]; then
        echo "────────────────────────────────────────────────"
        echo "  Add to ~/.zshrc (one-time):"
        echo "    export PATH=\"\$HOME/.grove/bin:\$PATH\""
        echo "  Then: source ~/.zshrc"
        echo "────────────────────────────────────────────────"
    fi

# Install gw as a uv tool (globally)
install:
    uv tool install . --force --reinstall

# Reinstall and reload shell integration
reload: install
    @echo 'Run: eval "$(gw shell-init)"'

# Build and run e2e tests in Docker
e2e:
    docker build -t grove-e2e -f e2e/Dockerfile .
    docker run --rm grove-e2e

# Tag a new release (usage: just release 0.4.0)
release version:
    #!/usr/bin/env bash
    set -euo pipefail
    current=$(python3 -c "import re; f=open('pyproject.toml').read(); print(re.search(r'version\s*=\s*\"(.+?)\"', f).group(1))")
    if [ "$current" != "{{ version }}" ]; then
        echo "Error: pyproject.toml version ($current) does not match {{ version }}"
        echo "Update pyproject.toml first, then run this again."
        exit 1
    fi
    git tag -a "v{{ version }}" -m "Release {{ version }}"
    git push origin "v{{ version }}"
