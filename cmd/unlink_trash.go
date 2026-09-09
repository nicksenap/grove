package cmd

import (
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var unlinkTrashCmd = &cobra.Command{
	Use:    "unlink-trash PATH",
	Short:  "Remove a quarantined workspace directory",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	Annotations: map[string]string{
		offlineCommandAnnotation: "true",
	},
	Run: func(cmd *cobra.Command, args []string) {
		logging.Setup(false)
		if err := workspace.UnlinkTrashPath(args[0]); err != nil {
			logging.Warn("unlink-trash %s: %s", args[0], err)
			exitError(err.Error())
		}
	},
}
