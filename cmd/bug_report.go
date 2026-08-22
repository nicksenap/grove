package cmd

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

const bugReportRepo = "nicksenap/grove"

var bugReportPrint bool

var bugReportCmd = &cobra.Command{
	Use:   "bug-report",
	Short: "Open a pre-filled GitHub issue with diagnostics",
	Long: `Collects system info, workspace state, doctor output, and recent logs,
then opens a pre-filled GitHub issue in your browser for review before submitting.

Use --print to output the report to stdout instead of opening a browser.`,
	Run: func(cmd *cobra.Command, args []string) {
		report := collectReport()

		if bugReportPrint || !console.IsTerminal(os.Stdin) {
			fmt.Println(report)
			return
		}

		issueURL := fmt.Sprintf("https://github.com/%s/issues/new?title=%s&body=%s",
			bugReportRepo,
			url.QueryEscape("Bug: "),
			url.QueryEscape(report),
		)

		console.Info("Collected diagnostics. Opening GitHub issue in browser...")
		console.Info("Review the issue before submitting — it contains log output.")

		if err := openBrowser(issueURL); err != nil {
			fmt.Fprintln(os.Stderr)
			fmt.Println(report)
		}
	},
}

func collectReport() string {
	var buf bytes.Buffer

	// System info
	buf.WriteString("## Environment\n\n")
	fmt.Fprintf(&buf, "- **gw version:** %s\n", Version)
	fmt.Fprintf(&buf, "- **Go version:** %s\n", runtime.Version())
	fmt.Fprintf(&buf, "- **OS/Arch:** %s/%s\n", runtime.GOOS, runtime.GOARCH)

	if shell := os.Getenv("SHELL"); shell != "" {
		fmt.Fprintf(&buf, "- **Shell:** %s\n", shell)
	}

	// Git version
	if out, err := exec.Command("git", "--version").Output(); err == nil {
		fmt.Fprintf(&buf, "- **Git:** %s\n", strings.TrimSpace(string(out)))
	}

	// Workspace summary
	buf.WriteString("\n## Workspaces\n\n")
	if workspaces, err := state.Load(); err == nil {
		if len(workspaces) == 0 {
			buf.WriteString("No workspaces configured.\n")
		} else {
			fmt.Fprintf(&buf, "%d workspace(s)\n\n", len(workspaces))
			for _, ws := range workspaces {
				fmt.Fprintf(&buf, "- **%s** (branch: `%s`, repos: %d)\n", ws.Name, ws.Branch, len(ws.Repos))
			}
		}
	} else {
		fmt.Fprintf(&buf, "Error loading state: %s\n", err)
	}

	// Doctor output
	buf.WriteString("\n## Doctor\n\n")
	if issues, _, err := workspace.NewService().Doctor(false); err == nil {
		if len(issues) == 0 {
			buf.WriteString("All workspaces healthy.\n")
		} else {
			for _, issue := range issues {
				repo := ""
				if issue.Repo != nil {
					repo = fmt.Sprintf(" (repo: %s)", *issue.Repo)
				}
				fmt.Fprintf(&buf, "- **%s**%s: %s\n", issue.Workspace, repo, issue.Issue)
			}
		}
	} else {
		fmt.Fprintf(&buf, "Error running doctor: %s\n", err)
	}

	// Recent logs
	buf.WriteString("\n## Recent Logs\n\n")
	buf.WriteString("```\n")
	buf.WriteString(tailLog(50))
	buf.WriteString("```\n")

	// Description placeholder
	buf.WriteString("\n## Description\n\n")
	buf.WriteString("<!-- Describe what happened and what you expected -->\n")

	return buf.String()
}

func tailLog(n int) string {
	logPath := filepath.Join(logging.LogDir, "grove.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return "(no log file found)\n"
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "linux":
		return exec.Command("xdg-open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

func init() {
	bugReportCmd.Flags().BoolVar(&bugReportPrint, "print", false, "Print report to stdout instead of opening browser")
}
