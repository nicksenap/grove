# Binary Build Spec

## Goal

Ship `gw` as a standalone binary to eliminate Python/uv startup overhead (~80ms → ~30ms) and simplify installation (no runtime dependency).

## Current state

- Installed via `uv tool install` or `brew` (which uses uv under the hood)
- 80ms startup: ~20ms Python interpreter + ~40ms Rich/Typer imports + ~20ms grove modules
- Users need Python 3.12+ and uv available

## Options

### 1. Nuitka (recommended to evaluate first)

Compiles Python to C, then to native binary. Keeps full CPython compatibility.

```bash
nuitka --standalone --onefile --output-filename=gw src/grove/__main__.py
```

- **Pros:** True native binary, fastest startup, no runtime needed
- **Cons:** Slow build (~2-5 min), larger binary (~15-30MB), C compiler needed in CI
- **Expected startup:** ~20-30ms

### 2. PyApp

Rust-based self-extracting Python app runner by the uv/hatch team (Astral).

```bash
# Bundles a Python distribution + your package into a single binary
PYAPP_PROJECT_NAME=grove PYAPP_PROJECT_VERSION=0.7.0 cargo install pyapp
```

- **Pros:** Simple, maintained by Astral (same team as uv), small config surface
- **Cons:** First run extracts to cache (~200ms), subsequent runs faster, still runs Python internally
- **Expected startup:** ~40-50ms (after first run)

### 3. PyInstaller

Bundles interpreter + deps into a single executable or directory.

```bash
pyinstaller --onefile --name gw src/grove/__main__.py
```

- **Pros:** Well-known, lots of docs, handles most dependencies
- **Cons:** Anti-virus false positives on macOS/Windows, ~20MB binary, bootloader adds ~30ms
- **Expected startup:** ~40-50ms

### 4. Shiv / zipapp

Self-extracting zip archive with bundled dependencies.

```bash
shiv -c gw -o gw.pyz grove
```

- **Pros:** Tiny output, simple build, fast
- **Cons:** Still needs Python on the system, not a true standalone binary
- **Expected startup:** ~60ms (marginal improvement)

## Recommendation

Evaluate **Nuitka** first for best startup time. Fall back to **PyApp** if Nuitka's build complexity is too high for CI.

## CI integration

Add a GitHub Actions job to the release workflow:

```yaml
build-binary:
  strategy:
    matrix:
      os: [macos-13, macos-14, ubuntu-latest]  # x86 mac, arm mac, linux
  steps:
    - Build binary for platform
    - Upload as release artifact
    - Update Homebrew formula to download binary instead of pip-installing
```

## Migration path

1. Build binary in CI alongside existing pip/uv install
2. Benchmark startup time on all platforms
3. If acceptable, switch Homebrew formula from `uv tool install` to binary download
4. Keep `uv tool install grove` working for contributors who want editable installs

## Success criteria

- `gw --version` in ≤ 40ms on Apple Silicon
- Single binary, no runtime dependencies
- macOS (arm64 + x86_64) and Linux (x86_64) supported
- Release workflow fully automated
