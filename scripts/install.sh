#!/bin/sh
# Install gw (Grove) from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/nicksenap/grove/master/scripts/install.sh | sh
#
# Environment overrides:
#   GW_VERSION      release tag to install (default: latest), e.g. v1.1.15 or 1.1.15
#   GW_INSTALL_DIR  target directory (default: /usr/local/bin if writable, else ~/.local/bin)
set -eu

REPO="nicksenap/grove"

log() { printf '%s\n' "$*" >&2; }
fail() { log "error: $*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"; }
need curl
need tar

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) fail "unsupported OS: $os (use 'go install github.com/$REPO/cmd/gw@latest')" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture: $arch" ;;
esac

version="${GW_VERSION:-}"
if [ -z "$version" ]; then
  version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$version" ] || fail "could not determine latest release"
fi
version="v${version#v}"
bare="${version#v}"

install_dir="${GW_INSTALL_DIR:-}"
if [ -z "$install_dir" ]; then
  if [ -w /usr/local/bin ]; then
    install_dir=/usr/local/bin
  else
    install_dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$install_dir"

archive="gw_${bare}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

log "Downloading gw $version ($os/$arch)..."
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

expected=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || fail "no checksum found for $archive"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" gw
chmod +x "$tmp/gw"
mv "$tmp/gw" "$install_dir/gw"

log "Installed gw $version to $install_dir/gw"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) log ""; log "Note: $install_dir is not on your PATH. Add it with:"; log "  export PATH=\"$install_dir:\$PATH\"" ;;
esac

log ""
log "Next, enable shell integration (bash/zsh):"
log "  eval \"\$(gw shell-init)\""
log "See https://github.com/$REPO#2-add-shell-integration for fish and other shells."
