## What's New

### Multiple repo directories

Grove no longer ties you to a single repos directory. Configure as many source directories as you need:

```bash
gw init ~/dev ~/work/microservices
gw add-dir ~/other/repos
gw remove-dir ~/old/repos
```

### Deep repo discovery

`gw explore` scans your configured directories recursively (up to 3 levels deep) and shows all discovered repos grouped by source directory. Nested repos that aren't direct children of your configured dirs are highlighted.

### Type-to-search in all menus

All interactive picker menus now support type-to-search filtering — just start typing to narrow the list.

### Automatic config migration

Existing configs using the old `repos_dir` format are automatically migrated to the new `repo_dirs` list format on first load. No manual steps needed.

## Upgrading

```bash
brew upgrade grove
```

Your existing config will be migrated automatically.
