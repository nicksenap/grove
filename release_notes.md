## Bug Fix

**Hooks no longer break after upgrading Grove.** Previously, `gw dash install` wrote hook commands that referenced the version-specific Python interpreter path (e.g. `Cellar/grove/0.12.0/libexec/bin/python3`). When Homebrew or uv upgraded Grove to a new version, that path stopped existing and all hooks silently failed.

Hook commands now use the stable `gw` entry point, which survives upgrades.

### Upgrade action required

After upgrading to 0.12.1, re-install your hooks:

```sh
gw dash uninstall
gw dash install
```

This is a one-time step. Future upgrades will not require it.
