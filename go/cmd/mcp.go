package cmd

import (
	"github.com/nicksenap/grove/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpWorkspace string

var mcpServeCmd = &cobra.Command{
	Use:    "mcp-serve",
	Short:  "Start MCP stdio server for cross-workspace communication",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		if mcpWorkspace == "" {
			exitError("--workspace is required")
		}

		if err := mcp.RunServer(mcpWorkspace); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	mcpServeCmd.Flags().StringVarP(&mcpWorkspace, "workspace", "w", "", "Workspace name")
}
