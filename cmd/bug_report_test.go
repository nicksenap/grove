package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/redact"
)

func TestRecentLogsAreBoundedByCountAndAge(t *testing.T) {
	dir := t.TempDir()
	logging.LogDir = dir
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	content := strings.Join([]string{
		"2026-01-01 11:59:59 INFO - too old",
		"2026-01-02 10:00:00 INFO - recent one",
		"2026-01-02 11:00:00 INFO - recent two",
		"2026-01-02 11:30:00 INFO - recent three",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "grove.log"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	result := recentLogs(2, 24*time.Hour, now)
	if strings.Contains(result, "too old") || strings.Contains(result, "recent one") {
		t.Fatalf("logs were not bounded: %q", result)
	}
	if !strings.Contains(result, "recent two") || !strings.Contains(result, "recent three") {
		t.Fatalf("recent logs missing: %q", result)
	}
}

func TestRedactTextRedactsSensitiveValues(t *testing.T) {
	home := t.TempDir()
	input := strings.Join([]string{
		"path=" + filepath.Join(home, "private", "repo"),
		"token=abc123",
		"password: 'two words'",
		"Authorization: Bearer bearer-secret",
		`{"token":"json-secret"}`,
		"--api-key cli-secret",
		"SOCKET_API_KEY=env-secret",
		"url=HTTPS://user:pass@example.com/repo?token=query-secret#access_token=fragment-secret",
	}, " ")
	result := redact.Text(input, home)

	for _, secret := range []string{home, "abc123", "two words", "bearer-secret", "json-secret", "cli-secret", "env-secret", "user:pass", "query-secret", "fragment-secret"} {
		if strings.Contains(result, secret) {
			t.Fatalf("report leaked %q: %s", secret, result)
		}
	}
	if !strings.Contains(result, "~/private/repo") || !strings.Contains(result, "[REDACTED]") {
		t.Fatalf("expected useful redaction markers: %s", result)
	}
}

func TestWriteBugReportUsesPrivateFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeBugReport(path, "diagnostics"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}
}

func TestWriteBugReportRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("do not replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "report.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeBugReport(link, "diagnostics"); err == nil {
		t.Fatal("expected symlink destination to be rejected")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "do not replace" {
		t.Fatalf("symlink target changed: %q", data)
	}
}

func TestRecentLogsExcludeOldContinuationsAndMalformedLines(t *testing.T) {
	dir := t.TempDir()
	logging.LogDir = dir
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	content := strings.Join([]string{
		"malformed standalone secret",
		"2026-01-01 11:00:00 INFO - old entry",
		"old continuation",
		"2026-01-02 11:00:00 INFO - recent entry",
		"recent continuation",
		"2026-01-02 13:00:00 INFO - future entry",
		"future continuation",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "grove.log.1"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	result := recentLogs(500, 24*time.Hour, now)
	if strings.Contains(result, "malformed") || strings.Contains(result, "old continuation") || strings.Contains(result, "future entry") || strings.Contains(result, "future continuation") {
		t.Fatalf("old or unbounded lines included: %q", result)
	}
	if !strings.Contains(result, "recent entry") || !strings.Contains(result, "recent continuation") {
		t.Fatalf("recent multiline entry missing: %q", result)
	}
}

func TestCollectReport(t *testing.T) {
	dir := t.TempDir()
	logging.LogDir = dir
	oldConfigPath := config.ConfigPath
	config.ConfigPath = filepath.Join(dir, "config.toml")
	t.Cleanup(func() { config.ConfigPath = oldConfigPath })

	// Write a small log file
	now := time.Now().Format("2006-01-02 15:04:05")
	os.WriteFile(filepath.Join(dir, "grove.log"), []byte(now+" INFO - test token=private-value\n"), 0o600)

	report := collectReport()

	// Verify key sections exist
	sections := []string{
		"## Environment",
		"gw version",
		"Go version",
		"OS/Arch",
		"## Configuration",
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

	// Verify sanitized log content is included
	if !strings.Contains(report, "INFO - test token=[REDACTED]") || strings.Contains(report, "private-value") {
		t.Fatalf("report should contain sanitized log content: %s", report)
	}
	if !strings.Contains(report, "Review this report before sharing") {
		t.Error("report should include a review warning")
	}
}
