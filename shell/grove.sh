#!/usr/bin/env bash
# Grove shell integration — wraps gw commands with cd support.
# Add to your shell config: eval "$(gw shell-init)"

gw() {
    # Only capture output for commands that need cd interception.
    # Everything else passes through directly (preserving prompts/colors).

    if [ "$1" = "go" ]; then
        local output
        output="$(command gw "$@" 2>&1)"
        local rc=$?
        if [ $rc -eq 0 ] && [ -n "$output" ] && [ -d "$output" ]; then
            cd "$output" || return 1
        else
            echo "$output"
        fi
        return $rc
    fi

    # For create --go, use a temp file so stdout stays connected to the tty
    local has_go=false
    for arg in "$@"; do
        [ "$arg" = "--go" ] && has_go=true
    done

    if [ "$has_go" = true ]; then
        local tmpfile
        tmpfile="$(mktemp)"
        command gw "$@" | tee "$tmpfile"
        local rc=${PIPESTATUS[0]}
        local cd_line
        cd_line="$(grep '^__grove_cd:' "$tmpfile")"
        rm -f "$tmpfile"
        if [ -n "$cd_line" ]; then
            local dir="${cd_line#__grove_cd:}"
            [ -d "$dir" ] && cd "$dir" || return 1
        fi
        return $rc
    fi

    # Default: pass through directly
    command gw "$@"
}
