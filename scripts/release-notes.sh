#!/usr/bin/env bash
# Extract the latest version section from CHANGELOG.md for goreleaser.
# Prints everything between the first ## heading and the next one (or EOF).
set -euo pipefail

changelog="${1:-CHANGELOG.md}"

if [ ! -f "$changelog" ]; then
  exit 0
fi

awk '/^## /{if(found) exit; found=1; next} found{print}' "$changelog"
