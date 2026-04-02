package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCreatesLogFile(t *testing.T) {
	dir := t.TempDir()
	LogDir = dir

	Setup(true)

	logFile := filepath.Join(dir, "grove.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("log file should be created")
	}
}

func TestDebugWritesToFile(t *testing.T) {
	dir := t.TempDir()
	LogDir = dir

	Setup(true)
	Debug("test message %d", 42)

	data, err := os.ReadFile(filepath.Join(dir, "grove.log"))
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "test message 42") {
		t.Errorf("log should contain message, got: %q", content)
	}
	if !strings.Contains(content, "DEBUG") {
		t.Errorf("log should contain level, got: %q", content)
	}
}

func TestNoWriteWhenNotVerbose(t *testing.T) {
	dir := t.TempDir()
	LogDir = dir

	Setup(false)
	Debug("should not appear")

	logFile := filepath.Join(dir, "grove.log")
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		// File may exist but should be empty or not exist
		data, _ := os.ReadFile(logFile)
		if strings.Contains(string(data), "should not appear") {
			t.Error("debug messages should not be written when verbose is false")
		}
	}
}

func TestLogRotation(t *testing.T) {
	dir := t.TempDir()
	LogDir = dir

	Setup(true)

	// Write enough to exceed 1MB
	bigMsg := strings.Repeat("x", 1024)
	for i := 0; i < 1100; i++ {
		Debug("%s", bigMsg)
	}

	// Should have created backup files
	entries, _ := os.ReadDir(dir)
	logFiles := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "grove.log") {
			logFiles++
		}
	}
	if logFiles < 2 {
		t.Errorf("expected at least 2 log files (rotation), got %d", logFiles)
	}
}
