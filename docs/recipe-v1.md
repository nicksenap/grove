# Recipe v1 specification

## Objective

Define a strict YAML description of repositories and local preparation jobs. Validation is independently available before [workspace creation and execution](recipe-execution.md).

Recipe uses GitHub Actions-inspired vocabulary (`jobs`, `needs`, `steps`, `name`, and `run`) to reduce the learning curve. It is a Grove-specific schema and is not syntactically, semantically, or tooling-compatible with GitHub Actions.

## Command

```bash
gw recipe validate <file>
gw recipe validate <file> --json
```

Validation is offline: it does not load Grove config, contact the network, write logs, or mutate state.

## Schema

```yaml
version: 1
name: example-stack

repositories:
  api:
    url: https://github.com/acme/example-api.git
    ref: main
  web:
    url: https://github.com/acme/example-web.git
    ref: main

jobs:
  setup-api:
    repository: api
    steps:
      - name: Install API dependencies
        run: make setup
      - name: Build API
        run: make build

  setup-web:
    repository: web
    steps:
      - name: Install web dependencies
        run: make setup

  verify:
    repository: api
    needs: [setup-api, setup-web]
    steps:
      - run: make check
```

See [`examples/recipes/example-stack.yaml`](../examples/recipes/example-stack.yaml) for the complete generic example.

### Fields

- `version` — required integer; v1 accepts only `1`.
- `name` — optional non-empty display name, maximum 128 characters when present.
- `repositories` — required non-empty map of repository IDs to:
  - `url` — required Git URL using HTTP(S), SSH, `git://`, `file://`, or SCP syntax.
  - `ref` — required branch, tag, or commit name.
- `jobs` — optional map of job IDs; an omitted or empty map creates a repository-only Recipe. Each job contains:
  - `repository` — required repository ID.
  - `working-directory` — optional relative path inside the repository; defaults to its root.
  - `timeout-minutes` — optional integer from 1–360; execution defaults to 360 minutes.
  - `needs` — optional list of job IDs that must complete first.
  - `steps` — required non-empty ordered list of steps.
- Step fields:
  - `name` — optional display name.
  - `run` — required non-empty shell command.

Repository and job IDs must match `^[a-z][a-z0-9_-]{0,63}$`. Job map order has no execution meaning. `needs` defines ordering between jobs, while steps within one job are ordered sequentially.

`run` is a literal shell script. Recipe does not interpret GitHub Actions expressions such as `${{ ... }}` and does not support `on`, `runs-on`, `uses`, contexts, outputs, or marketplace actions. Validation never executes commands; future execution must treat Recipes as trusted code with normal host access.

## Strictness and limits

- Read only regular local files and accept one YAML document using YAML 1.2-compatible scalar behavior.
- Reject unknown fields, duplicate mapping keys, aliases, anchors, custom tags, additional documents, explicit nulls, and values with the wrong YAML type.
- Reject missing job/repository references, self-dependencies, duplicate `needs`, and DAG cycles.
- `working-directory` must be relative, clean, and may not escape with `..`.
- Maximum file size: 1 MiB.
- Maximum YAML nesting depth: 32; maximum parsed nodes: 100,000.
- Maximum repositories: 64.
- Maximum jobs: 256.
- Maximum steps per job: 64.
- Maximum dependencies per job: 64.

## Output contract

Human success:

```text
Recipe valid: example-stack (2 repositories, 3 jobs)
```

JSON success:

```json
{"valid":true,"name":"example-stack","version":1,"repositories":2,"jobs":3,"errors":[]}
```

JSON failure exits non-zero and writes only JSON to stdout:

```json
{"valid":false,"errors":[{"code":"unknown_job","path":"jobs.verify.needs[0]","line":28,"column":13,"message":"unknown job: build"}]}
```

Validation errors use stable `code`, `path`, `line`, `column`, and `message` fields. Unreadable files and malformed YAML use the same envelope.

## Compatibility

- Existing `gw create`, `-r/--repos`, `-p/--preset`, interactive presets, and `.grove.toml` remain unchanged.
- Presets and Recipes coexist in v1; there is no migration or deprecation.
- Familiar field names do not make Recipe files executable by GitHub Actions or vice versa.
- Future Recipe versions require an explicit parser branch. Unknown versions fail closed.

## Testing

- Table-driven parser and semantic validation tests.
- CLI tests for human output, JSON-only stdout, and exit behavior.
- A committed generic public example validated by the test suite.
- `just check` and `just e2e` remain green.

## Non-goals

Oven, TOML, includes, templates, expressions, triggers, runners, actions, variables, environment maps, cache declarations, conditions, matrices, retries, services, containers, remote files, or credential-provider machinery.
