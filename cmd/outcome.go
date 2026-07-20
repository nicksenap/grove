package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/nicksenap/grove/internal/workspace"
)

// renderRepoOutcomes prints each per-repository outcome to stderr so automation
// and humans both see complete detail before a non-zero exit. The public
// machine-readable envelope is a separate concern (issue #63).
func renderRepoOutcomes(res *workspace.OperationResult) {
	fprintRepoOutcomes(os.Stderr, res)
}

// fprintRepoOutcomes writes the ordered per-repository outcomes to w.
func fprintRepoOutcomes(w io.Writer, res *workspace.OperationResult) {
	for _, r := range res.Repos {
		line := fmt.Sprintf("  %-20s %s", r.RepoName, r.Status)
		if r.Phase != "" {
			line += " (" + r.Phase + ")"
		}
		if r.Err != nil {
			line += ": " + r.Err.Error()
		} else if r.Message != "" {
			line += ": " + r.Message
		}
		fmt.Fprintln(w, line)
	}
	if res.RecordID != "" {
		fmt.Fprintf(w, "  recovery record: %s (run: gw doctor)\n", res.RecordID)
	}
}
