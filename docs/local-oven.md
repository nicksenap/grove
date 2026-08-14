# Local Oven

The local Oven is an opt-in pool of prepared Recipe workspaces. It keeps one detached, ready slot per reconciled Recipe generation so workspace creation can claim prepared dependencies without rerunning Recipe jobs.

Cold Recipe creation remains unchanged and is always the fallback.

## Commands

```bash
gw oven bake recipe.yaml
gw oven reconcile recipe.yaml
gw oven status
gw oven status --json
gw oven clean [recipe.yaml]

gw create cake --branch feat/cake --recipe recipe.yaml --oven
```

- `bake` resolves the Recipe's refs and prepares a slot for the current generation if one is not already ready.
- `reconcile` fetches refs, resolves a fresh generation, ensures one ready slot, and only then removes older unclaimed ready/failed generations.
- `status` shows ready, active, failed, quarantined, and cleanup-blocked slots. Failure summaries never contain captured command output.
- `clean` removes safe unclaimed ready and failed slots. It refuses active, claimed, or quarantined slots.
- `create --oven` claims the newest ready generation known locally. A clean miss reports `Oven miss` and runs normal cold Recipe creation.

Schedule `gw oven reconcile recipe.yaml` with cron, `launchd`, systemd, or another external scheduler. Grove does not install or run a daemon.

## Freshness and generations

A Recipe key hashes the normalized Recipe plus local runner identity. A generation additionally hashes every resolved repository commit SHA. Map ordering and `needs` ordering do not affect identity; sequential step ordering does.

Only `bake` and `reconcile` fetch refs. An Oven hit intentionally does not contact remotes: it claims the newest generation previously made ready by reconciliation. This keeps the hit path fast and makes freshness an explicit scheduling policy.

Reconciliation bakes a replacement before removing the previous ready generation. If replacement preparation fails, the previous generation remains available for inspection, while `create --oven` falls back cold when no ready slot matches the current normalized Recipe key.

## Storage and visibility

Oven state is separate from normal workspace state:

```text
~/.grove/oven/
├── inventory.json
└── generations/<generation>/slots/<slot>/...
```

The Oven root is mode `0700`; its atomic inventory is mode `0600`. Slots use immutable physical backing paths. Claim creates a named symlink in `workspace_dir`, attaches the requested branches, and writes ordinary workspace state last. `state.json`, not directory existence, is the user-visible readiness authority.

Normal deletion verifies the external claim record, removes Git worktrees through the alias, removes the alias, and then removes the empty backing root and inventory record.

## Lifecycle and recovery

Slots use a bounded lifecycle:

```text
baking → ready → claiming → claimed
   └────→ failed/quarantined
claimed ─→ cleanup_failed
```

- Partial or interrupted bakes are never ready.
- A live bake carries its local process owner so concurrent reconciliation does not remove worktrees under preparation.
- Interrupted unmodified claims return to `ready`.
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
