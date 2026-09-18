package workspace

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/output"
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

type statusOutput struct {
	Workspace string                  `json:"workspace"`
	Path      string                  `json:"path"`
	Source    *models.WorkspaceSource `json:"source,omitempty"`
	Repos     []repoStatusResult      `json:"repos"`
}

type statusLine struct {
	Workspace string         `json:"workspace"`
	Repo      string         `json:"repo"`
	Path      string         `json:"path"`
	Branch    string         `json:"branch"`
	Status    string         `json:"status"`
	Changed   int            `json:"changed"`
	Ahead     string         `json:"ahead"`
	Behind    string         `json:"behind"`
	PR        *gitops.PRInfo `json:"pr,omitempty"`
}

func statusLines(ws *models.Workspace, results []repoStatusResult) []statusLine {
	lines := make([]statusLine, len(results))
	for i, result := range results {
		path := ""
		if i < len(ws.Repos) {
			path = ws.Repos[i].WorktreePath
		}
		lines[i] = statusLine{
			Workspace: ws.Name,
			Repo:      result.Repo,
			Path:      path,
			Branch:    result.Branch,
			Status:    result.Status,
			Changed:   result.Changed,
			Ahead:     result.Ahead,
			Behind:    result.Behind,
			PR:        result.PR,
		}
	}
	return lines
}

func writeStatus(w io.Writer, ws *models.Workspace, results []repoStatusResult, format output.Format, opts StatusOptions) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(w, statusOutput{Workspace: ws.Name, Path: ws.Path, Source: ws.Source, Repos: results})
	case output.JSONLines:
		return output.WriteJSONLines(w, statusLines(ws, results))
	case output.Name:
		for _, result := range results {
			fmt.Fprintln(w, result.Repo)
		}
		return nil
	case output.Path:
		for _, repo := range ws.Repos {
			fmt.Fprintln(w, repo.WorktreePath)
		}
		return nil
	case output.TSV:
		rows := [][]string{{"WORKSPACE", "REPO", "PATH", "BRANCH", "AHEAD", "BEHIND", "CHANGED", "STATUS"}}
		if opts.PR {
			rows[0] = append(rows[0], "PR")
		}
		for _, line := range statusLines(ws, results) {
			row := []string{line.Workspace, line.Repo, line.Path, line.Branch, line.Ahead, line.Behind, strconv.Itoa(line.Changed), formatStatus(line.Status)}
			if opts.PR {
				row = append(row, formatPR(line.PR))
			}
			rows = append(rows, row)
		}
		return output.WriteTSV(w, rows)
	case output.Table:
		printStatusTable(w, ws, results, opts)
		return nil
	default:
		return fmt.Errorf("unsupported status output %q", format)
	}
}

func printStatusTable(w io.Writer, ws *models.Workspace, results []repoStatusResult, opts StatusOptions) {
	fmt.Fprintf(w, "Workspace: %s  (%s)\n", ws.Name, ws.Path)
	if line := formatSourceLine(ws.Source); line != "" {
		fmt.Fprintf(w, "%s\n", line)
	}
	fmt.Fprintln(w)

	headers := []string{"Repo", "Branch", "↑↓", "Status"}
	if opts.PR {
		headers = []string{"Repo", "Branch", "↑↓", "PR", "Status"}
	}
	table := console.NewTable(w, headers)
	for _, result := range results {
		table.AddRow(statusRow(result, opts.PR))
	}
	table.Render()
}

func statusRow(rs repoStatusResult, withPR bool) []string {
	upDown := formatUpDown(rs.Ahead, rs.Behind)
	statusStr := formatStatus(rs.Status)
	if withPR {
		return []string{rs.Repo, rs.Branch, upDown, formatPR(rs.PR), statusStr}
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

func printVerboseStatus(w io.Writer, results []repoStatusResult, opts StatusOptions) {
	if !opts.Verbose {
		return
	}
	for _, result := range results {
		if result.Status != "clean" && result.Status != "" && !strings.HasPrefix(result.Status, "error:") {
			fmt.Fprintf(w, "\n%s:\n%s\n", result.Repo, result.Status)
		}
	}
}
