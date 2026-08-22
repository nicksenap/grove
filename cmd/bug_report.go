package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/redact"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

const (
	bugReportLogLines  = 500
	bugReportLogAge    = 24 * time.Hour
	logTimestampLayout = "2006-01-02 15:04:05"
)

var bugReportOutput string

var bugReportCmd = &cobra.Command{
	Use:   "bug-report",
	Short: "Print a sanitized diagnostic report",
	Long: `Collects system information, sanitized configuration, workspace health,
and recent logs, then prints a report for review. Grove never uploads the report.

Use --output to write the report to a private file instead of stdout. Always
review the report before sharing it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		report := collectReport()
		if bugReportOutput == "" {
			_, err := fmt.Fprint(cmd.OutOrStdout(), report)
			return err
		}
		if err := writeBugReport(bugReportOutput, report); err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Diagnostic report written to %s\nReview it before sharing.\n", bugReportOutput)
		return nil
	},
}

func collectReport() string {
	var buf bytes.Buffer

	buf.WriteString("<!-- Review this report before sharing. Grove has sanitized known sensitive values but cannot guarantee every private value was detected. -->\n\n")
	buf.WriteString("## Environment\n\n")
	fmt.Fprintf(&buf, "- **gw version:** %s\n", Version)
	fmt.Fprintf(&buf, "- **Go version:** %s\n", runtime.Version())
	fmt.Fprintf(&buf, "- **OS/Arch:** %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if out, err := exec.Command("git", "--version").Output(); err == nil {
		fmt.Fprintf(&buf, "- **Git:** %s\n", strings.TrimSpace(string(out)))
	}

	buf.WriteString("\n## Configuration\n\n")
	writeConfigSummary(&buf)

	buf.WriteString("\n## Workspaces\n\n")
	writeWorkspaceSummary(&buf)

	buf.WriteString("\n## Doctor\n\n")
	writeDoctorSummary(&buf)

	buf.WriteString("\n## Recent Logs\n\n")
	buf.WriteString("Up to 500 lines from the last 24 hours.\n\n```\n")
	logs := strings.ReplaceAll(recentLogs(bugReportLogLines, bugReportLogAge, time.Now()), "```", "`\u200b``")
	buf.WriteString(logs)
	buf.WriteString("```\n")

	buf.WriteString("\n## Description\n\n")
	buf.WriteString("<!-- Describe what happened and what you expected -->\n")

	home, _ := os.UserHomeDir()
	return redact.Text(buf.String(), home)
}

func writeWorkspaceSummary(buf *bytes.Buffer) {
	workspaces, err := state.Load()
	if err != nil {
		fmt.Fprintf(buf, "Error loading state: %s\n", err)
		return
	}
	if len(workspaces) == 0 {
		buf.WriteString("No workspaces configured.\n")
		return
	}
	fmt.Fprintf(buf, "%d workspace(s)\n\n", len(workspaces))
	for _, ws := range workspaces {
		fmt.Fprintf(buf, "- **%s** (branch: `%s`, repos: %d)\n", ws.Name, ws.Branch, len(ws.Repos))
	}
}

func writeDoctorSummary(buf *bytes.Buffer) {
	issues, _, err := workspace.NewService().Doctor(false)
	if err != nil {
		fmt.Fprintf(buf, "Error running doctor: %s\n", err)
		return
	}
	if len(issues) == 0 {
		buf.WriteString("All workspaces healthy.\n")
		return
	}
	for _, issue := range issues {
		repo := ""
		if issue.Repo != nil {
			repo = fmt.Sprintf(" (repo: %s)", *issue.Repo)
		}
		fmt.Fprintf(buf, "- **%s**%s: %s\n", issue.Workspace, repo, issue.Issue)
	}
}

func writeConfigSummary(buf *bytes.Buffer) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(buf, "Error loading configuration: %s\n", err)
		return
	}
	if cfg == nil {
		buf.WriteString("Grove is not initialized.\n")
		return
	}
	fmt.Fprintf(buf, "- **Repository directories:** %d\n", len(cfg.RepoDirs))
	fmt.Fprintf(buf, "- **Workspace directory:** `%s`\n", cfg.WorkspaceDir)
	fmt.Fprintf(buf, "- **Presets:** %d\n", len(cfg.Presets))
	if len(cfg.Hooks) == 0 {
		buf.WriteString("- **Hooks:** none\n")
		return
	}
	names := make([]string, 0, len(cfg.Hooks))
	for name := range cfg.Hooks {
		names = append(names, name)
	}
	sort.Strings(names)
	buf.WriteString("- **Hooks:**\n")
	for _, name := range names {
		hook := cfg.Hooks[name]
		policy := hook.OnFailure
		if policy == "" {
			policy = "warn"
		}
		fmt.Fprintf(buf, "  - `%s` (on_failure: `%s`, stream: `%t`, timeout: `%s`)\n", name, policy, hook.Stream, hook.Timeout)
	}
}

func recentLogs(limit int, maxAge time.Duration, now time.Time) string {
	base := filepath.Join(logging.LogDir, "grove.log")
	paths := []string{base + ".3", base + ".2", base + ".1", base}
	var lines []string
	cutoff := now.Add(-maxAge)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entryRecent := false
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line == "" {
				continue
			}
			if timestamp, ok := logTimestamp(line, now.Location()); ok {
				entryRecent = !timestamp.Before(cutoff) && !timestamp.After(now)
				if entryRecent {
					lines = append(lines, line)
				}
				continue
			}
			if entryRecent {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) == 0 {
		return "(no recent log entries found)\n"
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n") + "\n"
}

func logTimestamp(line string, location *time.Location) (time.Time, bool) {
	if len(line) < len(logTimestampLayout) {
		return time.Time{}, false
	}
	timestamp, err := time.ParseInLocation(logTimestampLayout, line[:len(logTimestampLayout)], location)
	return timestamp, err == nil
}

func writeBugReport(path, report string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating report directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspecting report path: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".grove-report-*")
	if err != nil {
		return fmt.Errorf("creating report: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("securing report: %w", err)
	}
	if _, err := tmp.WriteString(report); err != nil {
		tmp.Close()
		return fmt.Errorf("writing report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing report: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("installing report: %w", err)
	}
	return nil
}

func init() {
	bugReportCmd.Flags().StringVarP(&bugReportOutput, "output", "o", "", "Write report to a private file instead of stdout")
}
