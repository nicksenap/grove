package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellInitCmd = &cobra.Command{
	Use:   "shell-init",
	Short: "Print shell function for gw navigation",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(shellFunction)
	},
}

const shellFunction = `gw() {
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

    command gw "$@"
}
`
