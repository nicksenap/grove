#!/usr/bin/env bash
# Grove shell integration — wraps gw commands with cd support.
# Add to your shell config: eval "$(gw shell-init)"

gw() {
    # Only capture output for commands that need cd interception.
    # Everything else passes through directly (preserving prompts/colors).

    if [ "$1" = "go" ]; then
        local output
        output="$(command gw "$@")"
        local rc=$?
        if [ $rc -eq 0 ] && [ -n "$output" ] && [ -d "$output" ]; then
            cd "$output" || return 1
        else
            echo "$output"
        fi
        return $rc
    fi

    if [ "$1" = "create" ]; then
        local cdfile
        cdfile="$(mktemp "${TMPDIR:-/tmp}/.grove_cd.XXXXXX")"
        GROVE_CD_FILE="$cdfile" command gw "$@"
        local rc=$?
        if [ $rc -eq 0 ] && [ -s "$cdfile" ]; then
            local dir
            dir="$(cat "$cdfile")"
            [ -d "$dir" ] && cd "$dir"
        fi
        rm -f "$cdfile"
        return $rc
    fi

    # Default: pass through directly
    command gw "$@"
}
