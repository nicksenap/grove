## What's New

### `gw go --back` / `gw go --delete`

New flags for the `go` command to streamline workspace navigation:

- `gw go -b` — jump back to the source repo directory of the current workspace
- `gw go -d` — delete the current workspace after navigating away
- `gw go -bd` — go back and clean up in one step
- `gw go other-ws -d` — switch workspace and delete the old one

Cleanup runs as a background process so the `cd` is instant. If anything goes wrong, `gw doctor` catches it.

When the workspace spans repos from different directories, a picker lets you choose which one to go back to.

### Faster repo discovery

Repo discovery now runs in parallel with a disk cache, making `gw create` and interactive pickers noticeably snappier on setups with many repos.

## Upgrading

```bash
brew upgrade grove
```
