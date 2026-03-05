## Hotfix

### Fix type-to-search in interactive menus

Fixed a bug where menus required pressing `/` to enter search mode instead of filtering directly as you type.

### `gw list -s` flag

Show a status summary alongside your workspaces:

```bash
gw list -s
```

`status --all` is now deprecated in favor of `list -s`.

## Upgrading

```bash
brew upgrade grove
```
