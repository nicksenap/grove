# Local Oven

The local Oven is an opt-in pool of prepared Recipe workspaces. It keeps one detached, reusable template per reconciled Recipe generation so workspace creation can materialize prepared dependencies without rerunning Recipe jobs.

Cold Recipe creation remains unchanged and is always the fallback.

## Commands

```bash
gw oven bake recipe.yaml
gw oven reconcile recipe.yaml
gw oven status
gw oven status --json
gw oven clean [recipe.yaml]
gw oven schedule-example recipe.yaml --every 30m
gw oven schedule-example recipe.yaml --every 30m --format launchd|cron|systemd

gw create cake --branch feat/cake --recipe recipe.yaml --oven
```

- `bake` resolves the Recipe's refs and prepares a slot for the current generation if one is not already ready.
- `reconcile` fetches refs, resolves a fresh generation, ensures one reusable ready template, and only then removes older ready/failed templates. Active workspaces remain independent.
- `status` shows reusable templates plus active, failed, quarantined, and cleanup-blocked materializations. Failure summaries never contain captured command output.
- `clean` removes safe ready/failed templates. It refuses active, claimed, or quarantined materializations.
- `create --oven` materializes an independent workspace from the newest reusable template known locally. The template remains ready, so unchanged upstream commits support repeated instant creates without another reconcile. A clean miss reports `Oven miss` and runs normal cold Recipe creation.

Schedule `gw oven reconcile recipe.yaml` with cron, `launchd`, systemd, or another external scheduler. Grove does not install or run a daemon.

## External scheduling

Generate a scheduler example without installing anything:

```bash
# Defaults to launchd on macOS and systemd on Linux
gw oven schedule-example /absolute/path/recipe.yaml --every 30m

# Request another supported format
gw oven schedule-example /absolute/path/recipe.yaml --every 30m --format cron
gw oven schedule-example /absolute/path/recipe.yaml --every 30m --format systemd
```

The generated example contains canonical absolute paths for both the active `gw` executable and the Recipe, plus a stable Recipe-specific identifier in launchd/systemd names so multiple Recipes do not collide. Review the output before installing it. If you later switch between a Homebrew and source-built `gw`, regenerate the example so the scheduler invokes the intended binary.

### macOS launchd

```bash
gw oven schedule-example recipe.yaml --every 30m --format launchd \
  > ~/Library/LaunchAgents/com.grove.oven.<recipe-id>.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.grove.oven.<recipe-id>.plist
```

Unload it with:

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.grove.oven.<recipe-id>.plist
```

`launchd` is preferred to cron on laptops because its interval jobs resume after sleep. The example also runs once when loaded.

### cron

```bash
gw oven schedule-example recipe.yaml --every 30m --format cron
crontab -e
```

Copy the generated entry into the crontab. Cron examples support intervals that evenly divide an hour or day.

### Linux systemd user timer

```bash
gw oven schedule-example recipe.yaml --every 30m --format systemd
```

Split the generated service and timer sections into:

```text
~/.config/systemd/user/grove-oven.service
~/.config/systemd/user/grove-oven.timer
```

Then enable the timer:

```bash
systemctl --user daemon-reload
systemctl --user enable --now grove-oven.timer
```

Grove only prints examples. It does not write scheduler files, call `launchctl`, edit crontabs, enable systemd units, store scheduling state, or run a background process.

## Freshness and generations

A Recipe key hashes the normalized Recipe plus local runner identity. A generation additionally hashes every resolved repository commit SHA. Map ordering and `needs` ordering do not affect identity; sequential step ordering does.

Only `bake` and `reconcile` fetch refs. An Oven hit intentionally does not contact remotes: it materializes from the newest generation previously made ready by reconciliation. This keeps the hit path fast and makes freshness an explicit scheduling policy.

Reconciliation bakes a replacement before removing the previous ready template. If replacement preparation fails, the previous generation remains available, while `create --oven` falls back cold when no ready template matches the current normalized Recipe key.

## Storage and visibility

Oven state is separate from normal workspace state:

```text
~/.grove/oven/
├── inventory.json
└── generations/<generation>/slots/<slot>/...
```

The Oven root is mode `0700`; its atomic inventory is mode `0600`. Templates and claims use separate immutable backing paths. A claim creates exact-commit worktrees, materializes the template with native copy-on-write cloning when available (safe recursive copy otherwise), preserves each new worktree's Git administration, creates a named symlink in `workspace_dir`, attaches the requested branches, and writes ordinary workspace state last. `state.json`, not directory existence, is the user-visible readiness authority.

Normal deletion verifies the external claim record, removes only that claim's Git worktrees and alias, and then removes its backing root and inventory record. The reusable template remains ready.

## Lifecycle and recovery

Slots use a bounded lifecycle:

```text
template: baking → ready ───────────────→ removed when stale/cleaned
                    ├→ claiming → claimed
                    ├→ claiming → claimed
                    └→ claiming → claimed
claim:              └→ quarantined/cleanup_failed
```

- Partial or interrupted bakes are never ready.
- A live bake carries its local process owner so concurrent reconciliation does not remove worktrees under preparation.
- Interrupted unmodified claim materializations are rolled back and removed; their reusable template remains `ready`.
- A claim with complete matching workspace state finishes as `claimed`.
- Ambiguous aliases, branches, paths, state, or Git registrations become `quarantined`; Grove never guesses ownership.
- Sequential cleanup can be partial. Remaining records stay visible for deterministic reconciliation or explicit repair.

Claim, reconcile, clean, and normal workspace mutations share Grove's cross-process mutation lock.

## Ownership boundaries

Before claim or destructive cleanup, Grove verifies:

- trusted slot/generation identity and exact backing path;
- unique safe repository names and exact child paths;
- source repository, physical worktree, alias path, branch, and Git registration;
- exact detached commit before claim;
- complete workspace state against the external claim record.

`--force` does not bypass Oven ownership checks. To keep the external claim record exact, `rename`, `add-repo`, and `remove-repo` currently fail closed for Oven-backed workspaces. Normal Git commits, status, sync, and deletion remain supported.

## Trusted-code and secret boundary

Recipes remain trusted shell code with normal host access. Commands run outside Grove's mutation lock and stream output without persistent Oven logs. The inventory stores paths, commit identities, lifecycle state, and bounded error summaries—not environments, command output, credentials, or file contents.

Grove rejects ready slots containing common credential residue such as `.npmrc`, `.netrc`, `.pypirc`, Git credential files, private-key filenames, `.env` variants, and AWS/Docker credential paths outside dependency-cache directories. A detected file quarantines the slot without recording its contents.

Grove still cannot generically detect every secret created by trusted commands. Credential steps must use temporary files and remove them before the Recipe succeeds. A Recipe that leaves other credentials in its worktree must not be used for ready Oven slots.

## Non-goals

Remote Oven, containers, services, an installed daemon, credential-provider machinery, generic CI features, and generic artifact caching are not part of the local Oven.
