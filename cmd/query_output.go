package cmd

import (
	"encoding/csv"
	"encoding/json"
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

func writeJSON(w io.Writer, value any, lines bool) error {
	encoder := json.NewEncoder(w)
	if !lines {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func writeTSV(w io.Writer, rows [][]string) error {
	writer := csv.NewWriter(w)
	writer.Comma = '\t'
	if err := writer.WriteAll(rows); err != nil {
		return err
	}
	return writer.Error()
}

func writeWorkspaceList(w io.Writer, workspaces []models.Workspace, format output.Format) error {
	switch format {
	case output.JSON:
		return writeJSON(w, workspaces, false)
	case output.JSONLines:
		for _, ws := range workspaces {
			if err := writeJSON(w, ws, true); err != nil {
				return err
			}
		}
		return nil
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
		return writeTSV(w, rows)
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
		return writeJSON(w, summaries, false)
	case output.JSONLines:
		for _, summary := range summaries {
			if err := writeJSON(w, summary, true); err != nil {
				return err
			}
		}
		return nil
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
		return writeTSV(w, rows)
	case output.Table:
		home, _ := os.UserHomeDir()
		table := console.NewTable(w, []string{"Name", "Branch", "Repos", "Status", "Path"})
		for _, summary := range summaries {
			path := summary.Path
			if home != "" {
				path = strings.Replace(path, home, "~", 1)
			}
			table.AddRow([]string{summary.Name, summary.Branch, strconv.Itoa(summary.Repos), summary.Status, path})
		}
		table.Render()
		return nil
	default:
		return fmt.Errorf("unsupported workspace summary output %q", format)
	}
}

func writeWorkspaceShow(w io.Writer, ws *models.Workspace, format output.Format) error {
	switch format {
	case output.JSON, output.JSONLines:
		return writeJSON(w, ws, format == output.JSONLines)
	case output.Name:
		_, err := fmt.Fprintln(w, ws.Name)
		return err
	case output.Path:
		_, err := fmt.Fprintln(w, ws.Path)
		return err
	case output.TSV:
		return writeTSV(w, [][]string{
			{"NAME", "PATH", "BRANCH", "CREATED", "REPOS"},
			{ws.Name, ws.Path, ws.Branch, ws.CreatedAt, strconv.Itoa(len(ws.Repos))},
		})
	case output.Table:
		home, _ := os.UserHomeDir()
		wsPath := ws.Path
		if home != "" {
			wsPath = strings.Replace(wsPath, home, "~", 1)
		}
		created := ws.CreatedAt
		if len(created) > 19 {
			created = created[:19]
		}
		fmt.Fprintf(w, "Name:      %s\n", ws.Name)
		fmt.Fprintf(w, "Branch:    %s\n", ws.Branch)
		fmt.Fprintf(w, "Path:      %s\n", wsPath)
		fmt.Fprintf(w, "Created:   %s\n", created)
		fmt.Fprintf(w, "Repos:     %d\n\n", len(ws.Repos))

		wsPrefix := ws.Path + "/"
		table := console.NewTable(w, []string{"Repo", "Branch", "Worktree", "Source"})
		for _, repo := range ws.Repos {
			worktree := repo.WorktreePath
			if after, ok := strings.CutPrefix(worktree, wsPrefix); ok {
				worktree = after
			} else if home != "" {
				worktree = strings.Replace(worktree, home, "~", 1)
			}
			source := repo.SourceRepo
			if home != "" {
				source = strings.Replace(source, home, "~", 1)
			}
			table.AddRow([]string{repo.RepoName, repo.Branch, worktree, source})
		}
		table.Render()
		return nil
	default:
		return fmt.Errorf("unsupported workspace show output %q", format)
	}
}

func writeRepoList(w io.Writer, entries []repoEntry, format output.Format) error {
	switch format {
	case output.JSON:
		return writeJSON(w, entries, false)
	case output.JSONLines:
		for _, entry := range entries {
			if err := writeJSON(w, entry, true); err != nil {
				return err
			}
		}
		return nil
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
		return writeTSV(w, rows)
	case output.Table:
		home, _ := os.UserHomeDir()
		table := console.NewTable(w, []string{"Name", "Owner/Repo", "Path"})
		for _, entry := range entries {
			path := entry.Path
			if home != "" {
				path = strings.Replace(path, home, "~", 1)
			}
			table.AddRow([]string{entry.Name, entry.DisplayName, path})
		}
		table.Render()
		return nil
	default:
		return fmt.Errorf("unsupported repo list output %q", format)
	}
}
