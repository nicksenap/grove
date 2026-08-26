// Package e2e exercises the compiled gw binary as a user would.
//
// These tests replace e2e/run.sh. Each scenario gets its own temporary HOME
// and repositories, so tests are independently runnable with `go test ./e2e -run TestName`.
//
// Assertion mapping vs the Bash suite:
//   - Every deterministic Bash section has an equivalent Test*.
//   - "command succeeded" checks are implied by mustGW (non-zero exit fails).
//   - Isolated tests assert local repo counts (e.g. 1+1=2) rather than the Bash
//     suite's leftover shared-state counts (e.g. 3+1=4).
//   - The public GitHub HTTPS clone is opt-in via GROVE_EXTERNAL_E2E=1; the
//     required suite covers clone-from-URL with a local file:// remote.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gwBin is the absolute path to the gw binary under test.
var gwBin string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	bin := os.Getenv("GW_BIN")
	if bin == "" {
		root, err := moduleRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
			return 1
		}
		tmp, err := os.MkdirTemp("", "grove-e2e-bin-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "e2e: temp dir: %v\n", err)
			return 1
		}
		defer os.RemoveAll(tmp)
		bin = filepath.Join(tmp, "gw")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/gw")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "e2e: build gw: %v\n%s", err, out)
			return 1
		}
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: resolve GW_BIN: %v\n", err)
		return 1
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		fmt.Fprintf(os.Stderr, "e2e: gw binary not found at %s\n", abs)
		return 1
	}
	if st.Mode()&0o111 == 0 {
		fmt.Fprintf(os.Stderr, "e2e: gw binary is not executable: %s\n", abs)
		return 1
	}
	gwBin = abs
	return m.Run()
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
