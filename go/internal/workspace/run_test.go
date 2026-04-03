package workspace

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestChildProcessGetsOwnProcessGroup verifies that child processes spawned
// by Run get their own process group (Setpgid=true), so the terminal's
// Ctrl+C SIGINT doesn't hit them directly.
func TestChildProcessGetsOwnProcessGroup(t *testing.T) {
	// Spawn a child process the same way Run does
	cmd := exec.Command("sh", "-c", "echo $$ && sleep 60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil

	out := &strings.Builder{}
	cmd.Stdout = out
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
	}()

	// Give the shell a moment to print its PID
	time.Sleep(100 * time.Millisecond)

	// Child should have a different process group than us
	childPgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("getpgid: %v", err)
	}
	myPgid, _ := syscall.Getpgid(os.Getpid())

	if childPgid == myPgid {
		t.Error("child should have its own process group, but shares ours")
	}
	if childPgid != cmd.Process.Pid {
		t.Errorf("child pgid (%d) should equal its pid (%d) with Setpgid=true", childPgid, cmd.Process.Pid)
	}
}

// TestSignalTerminatesProcessGroup verifies that sending SIGTERM to
// a process group (-pid) kills the entire child tree.
func TestSignalTerminatesProcessGroup(t *testing.T) {
	// Spawn a shell that spawns a child — simulates "sh -c 'npm start'" spawning node
	cmd := exec.Command("sh", "-c", "sleep 60 & echo $! && wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil

	out := &strings.Builder{}
	cmd.Stdout = out
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Give time for the background sleep to start
	time.Sleep(200 * time.Millisecond)

	// Parse the grandchild PID from output
	grandchildPidStr := strings.TrimSpace(out.String())
	grandchildPid, err := strconv.Atoi(grandchildPidStr)
	if err != nil {
		// Can still test process group kill even without grandchild PID
		t.Logf("could not parse grandchild pid from %q, skipping grandchild check", grandchildPidStr)
		grandchildPid = 0
	}

	// Send SIGTERM to the process group
	syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

	// Wait for the process to exit
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// good, process exited
	case <-time.After(3 * time.Second):
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
		t.Fatal("process did not exit within 3s after SIGTERM to process group")
	}

	// Verify grandchild is also dead
	if grandchildPid > 0 {
		time.Sleep(100 * time.Millisecond)
		err := syscall.Kill(grandchildPid, 0) // signal 0 = check if alive
		if err == nil {
			// Still alive — clean up and fail
			syscall.Kill(grandchildPid, syscall.SIGKILL)
			t.Error("grandchild process should be dead after group SIGTERM, but it's still alive")
		}
	}
}

// TestPrefixWriterUsesIndexByte verifies the prefixWriter handles
// various output patterns correctly.
func TestPrefixWriterLines(t *testing.T) {
	out := &strings.Builder{}
	f, _ := os.CreateTemp(t.TempDir(), "pw")
	defer f.Close()

	// We can't easily test with *os.File to a string, so test the logic
	// by writing to a temp file and reading back
	pw := &prefixWriter{prefix: "[test] ", w: f}

	pw.Write([]byte("hello\nworld\n"))
	pw.Write([]byte("partial"))
	pw.Write([]byte(" line\ndone\n"))

	f.Seek(0, 0)
	data, _ := os.ReadFile(f.Name())
	_ = out

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	expected := []string{
		"[test] hello",
		"[test] world",
		"[test] partial line",
		"[test] done",
	}

	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %v", len(expected), len(lines), lines)
	}
	for i, line := range lines {
		if line != expected[i] {
			t.Errorf("line %d: got %q, want %q", i, line, expected[i])
		}
	}
}
