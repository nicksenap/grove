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
