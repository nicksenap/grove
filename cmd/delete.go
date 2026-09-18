package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/nicksenap/grove/internal/operations"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	deleteStdin bool
	deleteYes   bool
)

// deleteCmd is the top-level "gw delete" command.
var deleteCmd = &cobra.Command{
	Use:   "delete [NAME...]",
	Short: "Delete workspaces (shortcut for gw ws delete)",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doDelete(cmd, args)
	},
}

// wsDeleteCmd is "gw ws delete".
var wsDeleteCmd = &cobra.Command{
	Use:   "delete [NAME...]",
	Short: "Delete workspaces",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doDelete(cmd, args)
	},
}

func doDelete(cmd *cobra.Command, args []string) {
	var names []string

	if len(args) > 0 || deleteStdin {
		var err error
		names, err = resolveDeleteNames(args, deleteStdin, deleteYes, cmd.InOrStdin())
		if err != nil {
			exitError(err.Error())
		}
	} else {
		workspaces, err := state.Load()
		if err != nil {
			exitError(err.Error())
		}
		if len(workspaces) == 0 {
			exitError("No workspaces to delete")
		}
		choices := make([]string, len(workspaces))
		for i, ws := range workspaces {
			choices[i] = ws.Name
		}
		selected, err := picker.PickMany("Select workspaces to delete:", choices)
		if err != nil {
			exitOnPickerErr(err)
		}
		names = selected
	}

	for _, name := range names {
		if _, err := operations.NewService().Delete(operations.DeleteRequest{
			Name: name, Options: workspace.RemoveOptions{Force: true},
		}); err != nil {
			exitError(err.Error())
		}
	}
}

func resolveDeleteNames(args []string, fromStdin, yes bool, stdin io.Reader) ([]string, error) {
	if !fromStdin {
		if yes {
			return nil, fmt.Errorf("--yes only applies with --stdin")
		}
		return uniqueDeleteNames(args), nil
	}
	if len(args) > 0 {
		return nil, fmt.Errorf("workspace arguments cannot be combined with --stdin")
	}
	if !yes {
		return nil, fmt.Errorf("--yes is required when deleting workspaces from --stdin")
	}

	names, err := readDeleteNames(stdin)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no workspace names received on stdin")
	}
	return names, nil
}

func readDeleteNames(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	var names []string
	for scanner.Scan() {
		names = append(names, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading workspace names from stdin: %w", err)
	}
	return uniqueDeleteNames(names), nil
}

func uniqueDeleteNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	return unique
}

func init() {
	for _, command := range []*cobra.Command{deleteCmd, wsDeleteCmd} {
		command.Flags().BoolVar(&deleteStdin, "stdin", false, "Read newline-delimited workspace names from stdin")
		command.Flags().BoolVarP(&deleteYes, "yes", "y", false, "Confirm deletion of workspace names read from stdin")
	}
	deleteCmd.ValidArgsFunction = completeWorkspaceNames
	wsDeleteCmd.ValidArgsFunction = completeWorkspaceNames
}
