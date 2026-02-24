#!/usr/bin/env bash
# Grove shell integration — wraps `gw go` with cd.
# Add to your shell config: eval "$(gw shell-init)"

gw() {
    if [ "$1" = "go" ] && [ -n "$2" ]; then
        local dir
        dir="$(command gw go "$2" 2>/dev/null)"
        if [ -n "$dir" ] && [ -d "$dir" ]; then
            cd "$dir" || return 1
        else
            command gw go "$2"
        fi
    else
        command gw "$@"
    fi
}
