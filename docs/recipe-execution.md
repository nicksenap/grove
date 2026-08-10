# Recipe workspace creation

> **Security:** A Recipe is trusted executable code, equivalent to running its shell scripts directly. `--recipe` is the explicit acknowledgement to execute it. Working-directory checks are not a sandbox; commands inherit Grove's environment and normal host access. Review Recipes from the same trust boundary as source code before running them.

## Command

```bash
gw create NAME --branch BRANCH --recipe recipe.yaml
gw create NAME --branch BRANCH --recipe recipe.yaml --json
```

`--recipe` is mutually exclusive with `--repos`, `--preset`, `--all`, `--replace`, `--track`, and `--force`. Without `--recipe`, create and Preset behavior is unchanged.

## Repository resolution

- Match each Recipe URL to configured local repositories by canonical remote identity, independent of HTTPS versus SSH spelling.
- Fail if multiple local repositories match one Recipe repository.
- Clone an unmatched URL into the first configured `repo_dir` using Grove's existing clone path.
- Use the Recipe repository ID as the workspace directory and state name.
- If the requested workspace branch already exists at the resolved base SHA, reuse it but persist that it is pre-existing so normal workspace deletion preserves it. A branch at any other commit fails closed.
- Before workspace mutation, require a successful fetch and resolve each Recipe `ref` to a commit SHA. Unqualified branches and `refs/heads/*` resolve only through fetched `origin` refs, tags resolve through exact tag refs, and hexadecimal object IDs are disambiguated as Git objects rather than ref names. Revision expressions and local-branch fallback are rejected.
- Canonical identity includes lowercase host plus case-preserving repository path. Resolution inspects every configured candidate without basename deduplication.

## Execution

- Recipe creation skips per-repository `.grove.toml` setup commands because the Recipe owns the complete bake. Ordinary create still runs them.
- Recipe jobs run after worktrees and state exist, outside Grove's mutation lock.
- Up to four jobs run concurrently.
- Independent jobs in different repositories may overlap. Only one job per repository runs at a time; when several are ready for one repository, lexical job ID wins.
- `needs` controls readiness; job map order has no meaning.
- Steps within a job run sequentially in separate `/bin/sh -c` processes.
- A job's `working-directory` applies to every step and is relative to its worktree.
- Commands inherit Grove's environment, receive no interactive stdin, and stream raw command output with sanitized job/step metadata prefixes.
- Optional `timeout-minutes` follows the familiar GitHub Actions spelling. It accepts 1–360 and defaults to 360 minutes per job.
- Global `post_create` runs only after every Recipe job succeeds.

## Failure and cancellation

- The first failed or timed-out step cancels queued work and kills ordinary running process groups. Deliberately detached/daemonized processes are outside v1's best-effort cancellation boundary.
- Failure identifies the job, one-based step number, optional step name, and cause.
- Grove retains the invocation's worktree and branch ownership metadata. On failure it reacquires the lock, verifies persisted workspace state still exactly matches the workspace it created, then removes only invocation-owned worktrees and branches.
- If state changed concurrently, cleanup fails closed and leaves recoverable state rather than guessing ownership. Cleanup errors are reported separately from the original preparation failure.
- SIGINT or SIGTERM cancels execution and uses the same rollback path.
- Cold creation writes no persistent Recipe logs; prefixed live output is the v1 log surface.

## JSON

Progress and command output remain on stderr. `--json` writes one result object to stdout.

Success:

```json
{"created":true,"name":"cake","path":"/workspaces/cake","recipe":"example-stack","jobs":[{"id":"setup-api","status":"succeeded","steps":2}]}
```

Failure exits non-zero after rollback:

```json
{"created":false,"name":"cake","error":{"code":"recipe_step_failed","job":"setup-api","step":2,"step_name":"Build API","message":"exit status 1"}}
```

Stable failure codes cover `recipe_invalid`, `repository_resolution_failed`, `workspace_provision_failed`, `recipe_step_failed`, `recipe_job_timeout`, `recipe_cancelled`, and `post_create_failed`. A cleanup failure adds `cleanup_error` while preserving the original job failure. An aborting global `post_create` failure reports `created: true` because the completed workspace remains present.

## Non-goals

Oven slots, remote execution, containers, services, matrices, retries, conditions, actions, expressions, output caching, persistent logs, or credential-provider machinery.
