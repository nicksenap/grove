Prepare a release for Grove.

1. Read `CHANGELOG.md` and `pyproject.toml` or `go.mod` for the current version context.
2. Run `git log --oneline $(git describe --tags --abbrev=0)..HEAD` to see all commits since the last tag.
3. Draft a new `## vX.Y.Z` section for the top of `CHANGELOG.md` based on those commits. Group changes by theme (features, fixes, cleanup). Write user-facing descriptions, not commit messages. Skip docs/test/ci-only changes.
4. Show me the draft and ask for the version number before writing.
5. After approval, prepend the new section to `CHANGELOG.md` (below the `# Changelog` heading, above the previous version).
6. Do NOT commit, tag, or push — I'll handle that.
