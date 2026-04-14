# Grove shell integration for Nushell
# Wraps gw commands with cd support for "go" and "create" subcommands.
#
# Option 1 — generate from gw:
#   gw shell-init --shell nu | save -f ~/.config/nushell/grove.nu
#   # then add to config.nu: source grove.nu
#
# Option 2 — source this file directly:
#   # in config.nu: source /path/to/grove/shell/grove.nu

def --env --wrapped gw [...args: string] {
    if ($args | length) > 0 and $args.0 == "go" {
        let output = (^gw ...$args | str trim)
        if ($output | path exists) {
            cd $output
        } else {
            print $output
        }
        return
    }

    if ($args | length) > 0 and $args.0 == "create" {
        let tmpdir = ($env.TMPDIR? | default "/tmp")
        let tmp = (^mktemp $"($tmpdir)/.grove_cd.XXXXXX" | str trim)
        with-env {GROVE_CD_FILE: $tmp} { ^gw ...$args }
        let dir = if ($tmp | path exists) {
            (open --raw $tmp | str trim)
        } else {
            ""
        }
        ^rm -f $tmp
        if $dir != "" {
            if ($dir | path exists) {
                cd $dir
            } else {
                print $"grove: workspace path does not exist: ($dir)"
            }
        }
        return
    }

    ^gw ...$args
}
