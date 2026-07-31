package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/announce"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

// Cross-workspace coordination for concurrent agents: `gw announce` publishes a
// note about a repo, `gw announcements` reads what other workspaces published
// about the repos you are touching. Recent notes also appear in `gw context`, so
// an agent receives them while orienting instead of having to remember to ask.

var (
	announceRepos    string
	announceCategory string
	announceMessage  string

	announcementsRepos  string
	announcementsSince  string
	announcementsAll    bool
	announcementsLimit  int
	announcementsGlobal bool
)

var announceCmd = &cobra.Command{
	Use:   "announce",
	Short: "Publish a note about a repo for agents in other workspaces",
	Long: `Publish a coordination note that agents working on the same repos in other
workspaces will see.

Repos default to every repo in the current workspace. Notes are keyed by each
repo's normalized remote, so a different worktree of the same upstream matches.
Notes expire after 30 days.

Categories: ` + strings.Join(announce.Categories(), ", "),
	Example: `  gw announce -c breaking_change -m "auth tokens are now opaque strings"
  gw announce -r api-gateway -c warning -m "staging deploy is broken" --format json`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(announceMessage) == "" {
			fail(machine.Errorf(machine.CodeUsage, "message is required").
				WithFix(`Pass --message / -m "what other agents need to know"`))
		}
		if announceCategory == "" {
			announceCategory = announce.CategoryInfo
		}

		ws, repos := resolveAnnounceTargets(announceRepos)

		svc := workspace.NewService()
		published := make([]*announce.Announcement, 0, len(repos))
		for _, key := range repos {
			a, err := svc.Announce.Publish(ws, key, announceCategory, announceMessage)
			if err != nil {
				fail(classifyAnnounceErr(err))
			}
			published = append(published, a)
		}

		if !machine.Enabled() {
			console.Successf("Announced to %d repo(s): %s", len(published), strings.Join(repos, ", "))
			return
		}
		machine.Emit(map[string]any{"published": published, "count": len(published)},
			machine.NextAction("Read what other workspaces published", "gw announcements --format json"))
	},
}

var announcementsCmd = &cobra.Command{
	Use:     "announcements",
	Aliases: []string{"news"},
	Short:   "Read notes other workspaces published about your repos",
	Long: `Read coordination notes published by agents in other workspaces.

By default this shows notes about the repos in the current workspace, excluding
your own. Recent notes also appear in "gw context".`,
	Example: `  gw announcements
  gw announcements --since 24h --format json
  gw announcements --global --format json`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		opts := announce.ListOptions{Limit: announcementsLimit}

		if !announcementsGlobal {
			ws, repos := resolveAnnounceTargets(announcementsRepos)
			opts.Repos = repos
			if !announcementsAll {
				opts.ExcludeWorkspace = ws
			}
		}

		if announcementsSince != "" {
			d, err := time.ParseDuration(announcementsSince)
			if err != nil {
				fail(machine.Wrap(machine.CodeUsage, err, "invalid --since %q", announcementsSince).
					WithFix(`Use a Go duration, e.g. "2h" or "72h"`))
			}
			opts.Since = time.Now().UTC().Add(-d)
		}

		found, err := workspace.NewService().Announce.List(opts)
		if err != nil {
			fail(machine.Wrap(machine.CodeInternal, err, "reading announcements: %s", err))
		}

		if machine.Enabled() {
			machine.Emit(map[string]any{"announcements": found, "count": len(found)})
			return
		}

		if len(found) == 0 {
			console.Info("No announcements.")
			return
		}
		table := console.NewTable(os.Stdout, []string{"When", "Workspace", "Repo", "Category", "Message"})
		for _, a := range found {
			table.AddRow([]string{
				humanizeAge(time.Since(a.CreatedAt)),
				a.Workspace,
				a.Repo,
				a.Category,
				a.Message,
			})
		}
		table.Render()
	},
}

// resolveAnnounceTargets returns the publishing workspace name and the repo
// coordination keys to use.
//
// Keys come from each repo's remote via announce.RepoKey, and both publishing and
// reading go through this one function — if the two sides derived keys
// differently they would silently never see each other's notes.
func resolveAnnounceTargets(reposFlag string) (wsName string, keys []string) {
	var ws *models.Workspace
	if resolved, err := workspace.ResolveWorkspace(""); err == nil {
		ws = resolved
		wsName = resolved.Name
	}

	// Explicit repos: accept names (resolved against the workspace for their
	// remote) or full remote URLs, so a caller outside any workspace still works.
	if reposFlag != "" {
		for _, name := range parseRepoList(reposFlag) {
			keys = append(keys, keyForRepo(ws, name))
		}
		if len(keys) == 0 {
			fail(machine.Errorf(machine.CodeUsage, "--repos listed no usable repo"))
		}
		return wsName, keys
	}

	if ws == nil {
		fail(machine.Errorf(machine.CodeWorkspaceNotFound,
			"not inside a workspace, so there are no repos to use").
			WithFix("Pass --repos explicitly, or run from inside a workspace").
			WithActions(machine.NextAction("Discover current context", "gw context --format json")))
	}
	for _, r := range ws.Repos {
		keys = append(keys, announce.RepoKey(gitops.RemoteURL(r.WorktreePath, "origin"), r.RepoName))
	}
	if len(keys) == 0 {
		fail(machine.Errorf(machine.CodeRepoNotFound, "workspace %s has no repos", ws.Name))
	}
	return wsName, keys
}

// keyForRepo resolves one repo reference to its coordination key, preferring the
// remote of a matching repo in the workspace.
func keyForRepo(ws *models.Workspace, ref string) string {
	if ws != nil {
		if r := ws.FindRepo(ref); r != nil {
			return announce.RepoKey(gitops.RemoteURL(r.WorktreePath, "origin"), r.RepoName)
		}
	}
	return announce.NormalizeRepo(ref)
}

// classifyAnnounceErr maps store validation failures onto contract codes.
func classifyAnnounceErr(err error) error {
	var invalid *announce.InvalidCategoryError
	if errors.As(err, &invalid) {
		return machine.Wrap(machine.CodeUsage, err, "%s", err).
			WithFix("Use one of: " + strings.Join(announce.Categories(), ", "))
	}
	return machine.Wrap(machine.CodeInternal, err, "publishing announcement: %s", err)
}

// humanizeAge renders a coarse relative age for the human table.
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func init() {
	announceCmd.Flags().StringVarP(&announceRepos, "repos", "r", "",
		"Comma-separated repo names or remote URLs (default: every repo in the current workspace)")
	announceCmd.Flags().StringVarP(&announceCategory, "category", "c", announce.CategoryInfo,
		"Category: "+strings.Join(announce.Categories(), ", "))
	announceCmd.Flags().StringVarP(&announceMessage, "message", "m", "", "What other agents need to know")
	announceCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	announceCmd.RegisterFlagCompletionFunc("category",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return announce.Categories(), cobra.ShellCompDirectiveNoFileComp
		})

	announcementsCmd.Flags().StringVarP(&announcementsRepos, "repos", "r", "",
		"Comma-separated repo names or remote URLs (default: every repo in the current workspace)")
	announcementsCmd.Flags().StringVar(&announcementsSince, "since", "",
		`Only notes newer than this duration, e.g. "24h"`)
	announcementsCmd.Flags().BoolVar(&announcementsAll, "include-own", false,
		"Include notes published by the current workspace")
	announcementsCmd.Flags().BoolVar(&announcementsGlobal, "global", false,
		"Every repo, not just the current workspace's")
	announcementsCmd.Flags().IntVar(&announcementsLimit, "limit", 50, "Maximum notes to return")
}
