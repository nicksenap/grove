package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/output"
	"github.com/nicksenap/grove/internal/workspace"
)

var queryOutputFormats = []output.Format{
	output.Table,
	output.JSON,
	output.JSONLines,
	output.TSV,
	output.Name,
	output.Path,
}

func resolveQueryOutput(value string, legacyJSON bool) (output.Format, error) {
	return output.Resolve(value, legacyJSON, queryOutputFormats...)
}

func abbreviateHome(path, home string) string {
	if home == "" {
		return path
	}
	return strings.Replace(path, home, "~", 1)
}

func writeWorkspaceList(w io.Writer, workspaces []models.Workspace, format output.Format) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(w, workspaces)
	case output.JSONLines:
		return output.WriteJSONLines(w, workspaces)
	case output.Name:
		for _, ws := range workspaces {
			fmt.Fprintln(w, ws.Name)
		}
		return nil
	case output.Path:
		for _, ws := range workspaces {
			fmt.Fprintln(w, ws.Path)
		}
		return nil
	case output.TSV:
		rows := [][]string{{"NAME", "BRANCH", "REPOS", "CREATED"}}
		for _, ws := range workspaces {
			rows = append(rows, []string{ws.Name, ws.Branch, strconv.Itoa(len(ws.Repos)), ws.CreatedAt})
		}
		return output.WriteTSV(w, rows)
	case output.Table:
		table := console.NewTable(w, []string{"Name", "Branch", "Repos", "Created"})
		for _, ws := range workspaces {
			created := ws.CreatedAt
			if len(created) > 10 {
				created = created[:10]
			}
			table.AddRow([]string{ws.Name, ws.Branch, strconv.Itoa(len(ws.Repos)), created})
		}
		table.Render()
		return nil
	default:
		return fmt.Errorf("unsupported workspace list output %q", format)
	}
}

func writeWorkspaceSummaries(w io.Writer, summaries []workspace.WorkspaceSummary, format output.Format) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(w, summaries)
	case output.JSONLines:
		return output.WriteJSONLines(w, summaries)
	case output.Name:
		for _, summary := range summaries {
			fmt.Fprintln(w, summary.Name)
		}
		return nil
	case output.Path:
		for _, summary := range summaries {
			fmt.Fprintln(w, summary.Path)
		}
		return nil
	case output.TSV:
		rows := [][]string{{"NAME", "BRANCH", "REPOS", "STATUS", "PATH"}}
		for _, summary := range summaries {
			rows = append(rows, []string{summary.Name, summary.Branch, strconv.Itoa(summary.Repos), summary.Status, summary.Path})
		}
		return output.WriteTSV(w, rows)
	case output.Table:
		home, _ := os.UserHomeDir()
		table := console.NewTable(w, []string{"Name", "Branch", "Repos", "Status", "Path"})
		for _, summary := range summaries {
			table.AddRow([]string{summary.Name, summary.Branch, strconv.Itoa(summary.Repos), summary.Status, abbreviateHome(summary.Path, home)})
		}
		table.Render()
		return nil
	default:
		return fmt.Errorf("unsupported workspace summary output %q", format)
	}
}

func writeWorkspaceShow(w io.Writer, ws *models.Workspace, format output.Format) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(w, ws)
	case output.JSONLines:
		return output.WriteJSONLine(w, ws)
	case output.Name:
		_, err := fmt.Fprintln(w, ws.Name)
		return err
	case output.Path:
		_, err := fmt.Fprintln(w, ws.Path)
		return err
	case output.TSV:
		return output.WriteTSV(w, [][]string{
			{"NAME", "PATH", "BRANCH", "CREATED", "REPOS"},
			{ws.Name, ws.Path, ws.Branch, ws.CreatedAt, strconv.Itoa(len(ws.Repos))},
		})
	case output.Table:
		writeWorkspaceTable(w, ws)
		return nil
	default:
		return fmt.Errorf("unsupported workspace show output %q", format)
	}
}

func writeWorkspaceTable(w io.Writer, ws *models.Workspace) {
	home, _ := os.UserHomeDir()
	created := ws.CreatedAt
	if len(created) > 19 {
		created = created[:19]
	}
	fmt.Fprintf(w, "Name:      %s\n", ws.Name)
	fmt.Fprintf(w, "Branch:    %s\n", ws.Branch)
	fmt.Fprintf(w, "Path:      %s\n", abbreviateHome(ws.Path, home))
	fmt.Fprintf(w, "Created:   %s\n", created)
	fmt.Fprintf(w, "Repos:     %d\n\n", len(ws.Repos))

	workspacePrefix := ws.Path + "/"
	table := console.NewTable(w, []string{"Repo", "Branch", "Worktree", "Source"})
	for _, repo := range ws.Repos {
		worktree := repo.WorktreePath
		if relative, ok := strings.CutPrefix(worktree, workspacePrefix); ok {
			worktree = relative
		} else {
			worktree = abbreviateHome(worktree, home)
		}
		table.AddRow([]string{repo.RepoName, repo.Branch, worktree, abbreviateHome(repo.SourceRepo, home)})
	}
	table.Render()
}

func writeRepoList(w io.Writer, entries []repoEntry, format output.Format) error {
	switch format {
	case output.JSON:
		return output.WriteJSON(w, entries)
	case output.JSONLines:
		return output.WriteJSONLines(w, entries)
	case output.Name:
		for _, entry := range entries {
			fmt.Fprintln(w, entry.Name)
		}
		return nil
	case output.Path:
		for _, entry := range entries {
			fmt.Fprintln(w, entry.Path)
		}
		return nil
	case output.TSV:
		rows := [][]string{{"NAME", "OWNER/REPO", "PATH"}}
		for _, entry := range entries {
			rows = append(rows, []string{entry.Name, entry.DisplayName, entry.Path})
		}
		return output.WriteTSV(w, rows)
	case output.Table:
		home, _ := os.UserHomeDir()
		table := console.NewTable(w, []string{"Name", "Owner/Repo", "Path"})
		for _, entry := range entries {
			table.AddRow([]string{entry.Name, entry.DisplayName, abbreviateHome(entry.Path, home)})
		}
		table.Render()
		return nil
	default:
		return fmt.Errorf("unsupported repo list output %q", format)
	}
}
