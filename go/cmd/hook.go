package cmd

import (
	"encoding/json"
	"io"
	"os"

	"github.com/nicksenap/grove/internal/hook"
	"github.com/spf13/cobra"
)

var hookEvent string

var hookCmd = &cobra.Command{
	Use:    "_hook",
	Short:  "Internal hook handler for Claude Code",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		if hookEvent == "" {
			return
		}

		// Read JSON from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil || len(data) == 0 {
			return
		}

		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			return // silently ignore malformed JSON
		}

		hook.HandleEvent(hookEvent, payload)
	},
}

func init() {
	hookCmd.Flags().StringVar(&hookEvent, "event", "", "Event type")
}
