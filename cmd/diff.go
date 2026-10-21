package cmd

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

// forcePagerEnv makes `gw diff` page as if stdout were a terminal. It exists
// so e2e tests can exercise the pager without a pty.
const forcePagerEnv = "GROVE_FORCE_PAGER"

var diffCmd = &cobra.Command{
	Use:   "diff [--workspace NAME] [--no-pager] [<git diff args>...]",
	Short: "Show git diff across workspace repos as one diff",
	Long: `Runs git diff in every workspace repo and prints one combined diff, with
paths prefixed a/<repo>/ and b/<repo>/. All other arguments go to git diff
untouched (e.g. --staged, main...HEAD, -U10, -w). Auto-detects the workspace
from cwd unless --workspace is given.

Output goes once through git's pager (GIT_PAGER, core.pager, PAGER) when
stdout is a terminal. Exit code is non-zero if any repo fails; with
--exit-code or --quiet, also when any repo has differences.

Known limitations:
  --stat/--shortstat print one block per repo, not combined totals.
  --name-only, --name-status, --numstat and --raw print paths without the
    repo prefix.
  Pathspecs go to every repo; repos without a match print nothing.
  No rename detection across repos; commit SHAs resolve only in their own repo.
  External diff tools (GIT_EXTERNAL_DIFF) label files without the repo prefix.`,
	// git diff flags must reach git untouched; parseDiffArgs picks out ours.
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		opts, err := parseDiffArgs(args)
		if err != nil {
			exitError(err.Error())
		}
		// Global flags (gw --verbose diff) arrive here unparsed; apply them.
		if len(opts.rootFlags) > 0 {
			if err := rootCmd.PersistentFlags().Parse(opts.rootFlags); err != nil {
				exitError(err.Error())
			}
			logging.Setup(verbose)
		}
		if opts.help {
			cmd.Help()
			return
		}
		ws, err := workspace.ResolveWorkspace(opts.workspace)
		if err != nil {
			exitError(err.Error())
		}
		os.Exit(runDiff(ws, opts.gitArgs, opts.noPager))
	},
}

type diffOptions struct {
	workspace string
	noPager   bool
	help      bool
	rootFlags []string
	gitArgs   []string
}

// parseDiffArgs extracts gw's own flags (--workspace, --no-pager, -h/--help,
// and global boolean flags such as --verbose and -n) from the arguments before
// any `--`; everything else is passed to git diff.
func parseDiffArgs(args []string) (diffOptions, error) {
	var opts diffOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			opts.gitArgs = append(opts.gitArgs, args[i:]...)
			return opts, nil
		case arg == "--no-pager":
			opts.noPager = true
		case arg == "-h" || arg == "--help":
			opts.help = true
		case arg == "--workspace":
			if i+1 >= len(args) {
				return opts, errors.New("--workspace requires a workspace name")
			}
			i++
			opts.workspace = args[i]
		case strings.HasPrefix(arg, "--workspace="):
			opts.workspace = strings.TrimPrefix(arg, "--workspace=")
		case isRootBoolFlag(arg):
			opts.rootFlags = append(opts.rootFlags, arg)
		default:
			opts.gitArgs = append(opts.gitArgs, arg)
		}
	}
	return opts, nil
}

// isRootBoolFlag reports whether arg is one of gw's global boolean flags
// (--name, --name=value, or -x).
func isRootBoolFlag(arg string) bool {
	fs := rootCmd.PersistentFlags()
	var name string
	if long, ok := strings.CutPrefix(arg, "--"); ok {
		name, _, _ = strings.Cut(long, "=")
	} else if len(arg) == 2 && arg[0] == '-' {
		if f := fs.ShorthandLookup(arg[1:]); f != nil {
			name = f.Name
		}
	}
	f := fs.Lookup(name)
	return f != nil && f.Value.Type() == "bool"
}

// exitStatusOnly reports whether args ask git diff for an exit status
// (--exit-code, --quiet); git does not start a pager then.
func exitStatusOnly(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--exit-code" || a == "--quiet" {
			return true
		}
	}
	return false
}

// pagerDir returns the first repo worktree that exists, for resolving the
// pager from repo config, or "" if none does.
func pagerDir(ws *models.Workspace) string {
	for _, r := range workspace.SortedRepos(ws) {
		if info, err := os.Stat(r.WorktreePath); err == nil && info.IsDir() {
			return r.WorktreePath
		}
	}
	return ""
}

// runDiff writes the combined diff to stdout, through one pager when stdout
// is a terminal, and returns the exit code.
func runDiff(ws *models.Workspace, args []string, noPager bool) int {
	dir := pagerDir(ws)
	if noPager || dir == "" || exitStatusOnly(args) ||
		(!console.IsTerminal(os.Stdout) && os.Getenv(forcePagerEnv) == "") {
		return workspace.Diff(ws, args, os.Stdout, os.Stderr)
	}
	pager := gitPager(dir)
	if pager == "" {
		return workspace.Diff(ws, args, os.Stdout, os.Stderr)
	}

	r, w, err := os.Pipe()
	if err != nil {
		return workspace.Diff(ws, args, os.Stdout, os.Stderr)
	}
	p := exec.Command("sh", "-c", pager)
	p.Stdin = r
	p.Stdout = os.Stdout
	p.Stderr = os.Stderr
	p.Env = pagerEnv()
	if err := p.Start(); err != nil {
		r.Close()
		w.Close()
		return workspace.Diff(ws, args, os.Stdout, os.Stderr)
	}
	r.Close()
	// Like git, Ctrl-C must not kill gw while the pager owns the terminal: catch
	// SIGINT and keep waiting. Catching (not ignoring) leaves it at the default
	// for the git children, which still stop on Ctrl-C.
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt)
	defer signal.Stop(sigint)
	// Like git: color when paging. User args come after, so they win.
	code := workspace.Diff(ws, append([]string{"--color=always"}, args...), w, os.Stderr)
	w.Close()
	_ = p.Wait()
	return code
}

// gitPager resolves the pager git itself would use (GIT_PAGER, core.pager,
// PAGER, then its default). "" means do not page.
func gitPager(dir string) string {
	out, err := exec.Command("git", "-C", dir, "var", "GIT_PAGER").Output()
	if err != nil {
		return ""
	}
	pager := strings.TrimSpace(string(out))
	if pager == "cat" {
		return ""
	}
	return pager
}

// pagerEnv sets LESS and LV defaults the way git does when it starts a pager.
func pagerEnv() []string {
	env := os.Environ()
	if _, ok := os.LookupEnv("LESS"); !ok {
		env = append(env, "LESS=FRX")
	}
	if _, ok := os.LookupEnv("LV"); !ok {
		env = append(env, "LV=-c")
	}
	return env
}

func init() {
	// Registered for help and docs only; parseDiffArgs does the parsing.
	diffCmd.Flags().String("workspace", "", "Workspace name (default: detect from cwd)")
	diffCmd.Flags().Bool("no-pager", false, "Do not pipe output into a pager")
}
