package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/logging"
)

func TestTailLog(t *testing.T) {
	dir := t.TempDir()
	logging.LogDir = dir

	// Write a log file with 10 lines
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "2026-01-01 00:00:00 INFO - line "+string(rune('0'+i)))
	}
	content := strings.Join(lines, "\n") + "\n"
	os.WriteFile(filepath.Join(dir, "grove.log"), []byte(content), 0o644)

	// Tail last 3 lines
	result := tailLog(3)
	resultLines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(resultLines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(resultLines), resultLines)
	}

	// Tail more than available
	result = tailLog(100)
	resultLines = strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(resultLines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(resultLines))
	}
}

func TestTailLogMissing(t *testing.T) {
	logging.LogDir = filepath.Join(t.TempDir(), "nonexistent")

	result := tailLog(10)
	if !strings.Contains(result, "no log file found") {
		t.Errorf("expected 'no log file found', got %q", result)
	}
}

func TestCollectReport(t *testing.T) {
	// Set up a minimal environment for collectReport
	dir := t.TempDir()
	logging.LogDir = dir

	// Write a small log file
	os.WriteFile(filepath.Join(dir, "grove.log"), []byte("2026-01-01 00:00:00 INFO - test\n"), 0o644)

	report := collectReport()

	// Verify key sections exist
	sections := []string{
		"## Environment",
		"gw version",
		"Go version",
		"OS/Arch",
		"## Workspaces",
		"## Doctor",
		"## Recent Logs",
		"## Description",
	}
	for _, s := range sections {
		if !strings.Contains(report, s) {
			t.Errorf("report missing section: %s", s)
		}
	}

	// Verify log content is included
	if !strings.Contains(report, "INFO - test") {
		t.Error("report should contain log content")
	}
}
