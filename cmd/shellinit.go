package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellFlag string

var shellInitCmd = &cobra.Command{
	Use:   "shell-init",
	Short: "Print shell function for gw navigation",
	Long: `Print shell function for gw navigation.

For bash/zsh, add to your shell config:
  eval "$(gw shell-init)"

For nushell, save to a file and source it:
  gw shell-init --shell nu | save -f ~/.config/nushell/grove.nu
  # then add to config.nu: source grove.nu`,
	Run: func(cmd *cobra.Command, args []string) {
		switch shellFlag {
		case "nu", "nushell":
			fmt.Print(nushellFunction)
		default:
			fmt.Print(bashFunction)
		}
	},
}

func init() {
	shellInitCmd.Flags().StringVar(&shellFlag, "shell", "bash", "Shell type (bash, nu)")
}

const bashFunction = `gw() {
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

const nushellFunction = `# Grove shell integration for Nushell
# Wraps gw commands with cd support for "go" and "create" subcommands.
# Add to your config.nu: source grove.nu

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
`
