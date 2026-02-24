set dotenv-load := false

# List available recipes
default:
    @just --list

# Run all checks (lint + format + tests)
check: lint test

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

# Install gw as editable for development
dev:
    uv pip install -e .

# Install gw as a uv tool (globally)
install:
    uv tool install . --force --reinstall

# Reinstall and reload shell integration
reload: install
    @echo 'Run: eval "$(gw shell-init)"'
