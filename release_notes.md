## What's Changed

Robustness hardening for state management, validation, and error handling.

### Fixes
- **Atomic file writes** for `state.json` and `config.toml` — prevents corruption on crash or kill mid-write
- **Corrupt state recovery** — corrupt `state.json` now shows a helpful error instead of a raw traceback
- **Empty workspace name guard** — branches like `"/"` that produce empty names are now rejected
- **Preset name validation** — rejects dots, spaces, and other TOML-unsafe characters
- **Atomic rename** — `gw rename` updates state first with rollback on directory rename failure
- **Cleaner delete** — no misleading warnings after successful force cleanup; keeps state on partial failure for `gw doctor` recovery
