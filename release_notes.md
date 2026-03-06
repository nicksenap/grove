## What's New

### Smarter repo discovery

Repos are now identified by their git remote URL instead of folder name. The interactive picker shows `org/repo-name` derived from the remote, so same-named repos from different orgs are properly distinguished.

Deep scanning is now the default in interactive mode — nested repos that were previously invisible to `gw create` are now available in the picker.

Repos with the same remote URL are automatically deduped (direct children preferred over nested copies).

Non-interactive flags (`-r repo-name`) still use folder names for backward compatibility.

## Upgrading

```bash
brew upgrade grove
```
