package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
)

func formatPR(pr *gitops.PRInfo) string {
	if pr == nil {
		return "-"
	}
	switch pr.State {
	case "MERGED":
		return fmt.Sprintf("#%d merged", pr.Number)
	case "CLOSED":
		return fmt.Sprintf("#%d closed", pr.Number)
	case "OPEN":
		switch pr.ReviewDecision {
		case "APPROVED":
			return fmt.Sprintf("#%d ✓", pr.Number)
		case "CHANGES_REQUESTED":
			return fmt.Sprintf("#%d ✗", pr.Number)
		default:
			return fmt.Sprintf("#%d open", pr.Number)
		}
	default:
		return fmt.Sprintf("#%d %s", pr.Number, pr.State)
	}
}

// formatSourceLine renders a workspace's source provenance as a single line for
// status output, or "" if there is no source. e.g.
// "Source: github 1172 — Surface data source status  (https://github.com/...)".
func formatSourceLine(src *models.WorkspaceSource) string {
	if src == nil {
		return ""
	}
	label := src.Provider
	if label == "" {
		label = "source"
	}
	if src.Ref != "" {
		label += " " + src.Ref
	}
	if src.Title != "" {
		label += " — " + src.Title
	}
	if src.URL != "" {
		label += "  (" + src.URL + ")"
	}
	return "Source: " + label
}

func (s *Service) printStatusJSON(ws *models.Workspace, results []repoStatusResult) error {
	type wsStatus struct {
		Workspace string                  `json:"workspace"`
		Path      string                  `json:"path"`
		Source    *models.WorkspaceSource `json:"source,omitempty"`
		Repos     []repoStatusResult      `json:"repos"`
	}
	data, _ := json.MarshalIndent(wsStatus{
		Workspace: ws.Name,
		Path:      ws.Path,
		Source:    ws.Source,
		Repos:     results,
	}, "", "  ")
	fmt.Println(string(data))
	return nil
}

func (s *Service) printStatusTable(ws *models.Workspace, results []repoStatusResult, opts StatusOptions) {
	fmt.Fprintf(os.Stdout, "Workspace: %s  (%s)\n", ws.Name, ws.Path)
	if line := formatSourceLine(ws.Source); line != "" {
		fmt.Fprintf(os.Stdout, "%s\n", line)
	}
	fmt.Fprintln(os.Stdout)

	headers := []string{"Repo", "Branch", "↑↓", "Status"}
	if opts.PR {
		headers = []string{"Repo", "Branch", "↑↓", "PR", "Status"}
	}
	table := console.NewTable(os.Stdout, headers)

	for _, rs := range results {
		table.AddRow(statusRow(rs, opts.PR))
	}
	table.Render()
}

func statusRow(rs repoStatusResult, withPR bool) []string {
	upDown := formatUpDown(rs.Ahead, rs.Behind)
	statusStr := formatStatus(rs.Status)
	if withPR {
		prStr := "-"
		if rs.PR != nil {
			prStr = formatPR(rs.PR)
		}
		return []string{rs.Repo, rs.Branch, upDown, prStr, statusStr}
	}
	return []string{rs.Repo, rs.Branch, upDown, statusStr}
}

func formatUpDown(ahead, behind string) string {
	if ahead != "-" && behind != "-" && ahead != "" && behind != "" {
		return fmt.Sprintf("%s↑ %s↓", ahead, behind)
	}
	return "-"
}

func formatStatus(status string) string {
	if status == "clean" || status == "" || strings.HasPrefix(status, "error:") {
		return status
	}
	lines := strings.Count(status, "\n") + 1
	return fmt.Sprintf("%d changed", lines)
}

func (s *Service) printVerboseStatus(results []repoStatusResult, opts StatusOptions) {
	if !opts.Verbose {
		return
	}
	for _, rs := range results {
		if rs.Status != "clean" && rs.Status != "" && !strings.HasPrefix(rs.Status, "error:") {
			fmt.Fprintf(os.Stderr, "\n%s:\n%s\n", rs.Repo, rs.Status)
		}
	}
}
