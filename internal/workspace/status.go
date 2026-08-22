package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
)

type repoStatusResult struct {
	Repo   string         `json:"repo"`
	Branch string         `json:"branch"`
	Status string         `json:"status"`
	Ahead  string         `json:"ahead"`
	Behind string         `json:"behind"`
	PR     *gitops.PRInfo `json:"pr,omitempty"`
}

func collectRepoStatus(r models.RepoWorktree) repoStatusResult {
	rs := repoStatusResult{
		Repo:   r.RepoName,
		Branch: r.Branch,
	}
	if branch, err := gitops.CurrentBranch(r.WorktreePath); err == nil {
		if branch == "" {
			rs.Branch = "(detached)"
		} else {
			rs.Branch = branch
		}
	}
	status, err := gitops.RepoStatus(r.WorktreePath)
	if err != nil {
		rs.Status = "error: " + err.Error()
		rs.Ahead = "-"
		rs.Behind = "-"
		return rs
	}
	if status == "" {
		rs.Status = "clean"
	} else {
		rs.Status = status
	}

	upstream, _ := gitops.ResolveBaseBranch(r.SourceRepo)
	if upstream == "" {
		upstream = "origin/main"
	}
	ahead, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, upstream)
	if err == nil {
		rs.Ahead = fmt.Sprintf("%d", ahead)
		rs.Behind = fmt.Sprintf("%d", behind)
	} else {
		rs.Ahead = "-"
		rs.Behind = "-"
	}

	return rs
}

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

// StatusOptions controls status output.
type StatusOptions struct {
	JSON    bool
	Verbose bool
	PR      bool
}

// Status displays git status for a workspace.
func (s *Service) Status(wsName string, opts StatusOptions) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %s not found", wsName)
	}

	results := s.fetchStatusResults(ws.Repos, opts.PR)

	if opts.JSON {
		return s.printStatusJSON(ws, results)
	}

	s.printStatusTable(ws, results, opts)
	s.printVerboseStatus(results, opts)
	return nil
}

func (s *Service) fetchStatusResults(repos []models.RepoWorktree, withPR bool) []repoStatusResult {
	results := make([]repoStatusResult, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(idx int, repo models.RepoWorktree) {
			defer wg.Done()
			results[idx] = collectRepoStatus(repo)
			if withPR {
				results[idx].PR = gitops.PRStatus(repo.WorktreePath)
			}
		}(i, r)
	}
	wg.Wait()
	return results
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
