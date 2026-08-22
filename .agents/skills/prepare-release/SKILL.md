---
name: prepare-release
description: Prepare Grove release notes from commits since the latest tag. Use when asked to draft or prepare a release.
---

# Prepare a Grove release

1. Read `CHANGELOG.md`, `go.mod`, and the release process in `AGENTS.md` for current version context.
2. Run `git log --oneline $(git describe --tags --abbrev=0)..HEAD` to inspect commits since the latest tag.
3. Draft a new `## vX.Y.Z` section for the top of `CHANGELOG.md`. Group changes by theme such as features, fixes, and cleanup. Write user-facing descriptions rather than copying commit messages. Omit documentation-, test-, and CI-only changes.
4. Show the draft and ask the user to confirm the version before writing.
5. After approval, insert the section below the `# Changelog` heading and above the previous version.
6. Do not commit, tag, or push unless the user explicitly asks.
