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
    } else if ($args | length) > 0 and $args.0 == "create" {
        let tmp = (mktemp -t ".grove_cd.XXXXXX")
        with-env { GROVE_CD_FILE: $tmp } { ^gw ...$args }
        let dir = (open $tmp | str trim)
        if $dir != "" and ($dir | path exists) {
            cd $dir
        }
        rm -f $tmp
    } else {
        ^gw ...$args
    }
}
