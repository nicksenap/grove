# TODO

## Integration tests

Add end-to-end tests that exercise the main flows against real git repos (not mocks):
- `gw init` → `gw create` → `gw status` → `gw sync` → `gw delete`
- `gw create` → `gw add-repo` → `gw remove-repo`
- `gw create` → `gw rename` → `gw doctor`
- `gw create` → `gw run` (with actual processes)
- Lifecycle hooks fire in correct order with real `.grove.toml` files

## Recursive repo discovery

`gw init` currently only looks one level deep (`repos_dir/*.git`). If repos live in nested folders (e.g. `~/dev/work/backend/svc-auth`), they're invisible.

Make discovery recursive or configurable:
- Scan N levels deep (default 2–3?)
- Or accept multiple root dirs in config
- Show discovered repos during init so the user can confirm

## Non-interactive mode

Every command should be scriptable without interactive prompts (e.g. for CI, AI agents, shell scripts). Commands that currently fall back to interactive pickers when arguments are omitted need a `-y` / `--yes` flag or equivalent to skip confirmation and use sensible defaults. Audit all commands:
- `gw create` — branch picker, repo picker, preset picker, CLAUDE.md copy prompt
- `gw delete` — confirmation prompt
- `gw add-repo` — workspace picker, repo picker
- `gw remove-repo` — workspace picker, repo picker, confirmation prompt
- `gw rename` — workspace picker
- `gw go` — workspace picker
