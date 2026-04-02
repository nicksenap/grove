package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/config"
)

func setupTestEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	config.GroveDir = filepath.Join(dir, ".grove")
	os.MkdirAll(config.GroveDir, 0o755)
}

func TestSessionStartCreatesFile(t *testing.T) {
	setupTestEnv(t)

	payload := map[string]any{
		"session_id": "test-123",
		"cwd":        t.TempDir(),
		"model":      "claude-4",
	}
	HandleEvent("SessionStart", payload)

	path := statusPath("test-123")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("status file should exist")
	}

	var data StatusData
	raw, _ := os.ReadFile(path)
	json.Unmarshal(raw, &data)

	if data.Status != "IDLE" {
		t.Errorf("status: got %q, want IDLE", data.Status)
	}
	if data.SessionID != "test-123" {
		t.Errorf("session_id: got %q", data.SessionID)
	}
	if data.Model != "claude-4" {
		t.Errorf("model: got %q", data.Model)
	}
}

func TestSessionEndRemovesFile(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "end-test"})
	HandleEvent("SessionEnd", map[string]any{"session_id": "end-test"})

	path := statusPath("end-test")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("status file should be removed")
	}
}

func TestInvalidSessionIDRejected(t *testing.T) {
	setupTestEnv(t)

	// Path traversal attempt
	HandleEvent("SessionStart", map[string]any{"session_id": "../../../etc/passwd"})
	// Should not create any file
	path := statusPath("../../../etc/passwd")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("should reject path traversal session IDs")
	}
}

func TestEmptySessionIDRejected(t *testing.T) {
	setupTestEnv(t)
	HandleEvent("SessionStart", map[string]any{"session_id": ""})
	// No crash = pass
}

func TestPreToolUseSetsWorking(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "tool-test"})
	HandleEvent("PreToolUse", map[string]any{
		"session_id": "tool-test",
		"tool_name":  "Read",
	})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("tool-test"))
	json.Unmarshal(raw, &data)

	if data.Status != "WORKING" {
		t.Errorf("status: got %q, want WORKING", data.Status)
	}
	if data.ToolCount != 1 {
		t.Errorf("tool_count: got %d, want 1", data.ToolCount)
	}
	if data.LastTool != "Read" {
		t.Errorf("last_tool: got %q, want Read", data.LastTool)
	}
}

func TestPostToolUseFailureSetsError(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "err-test"})
	HandleEvent("PostToolUseFailure", map[string]any{
		"session_id": "err-test",
		"error":      "file not found",
	})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("err-test"))
	json.Unmarshal(raw, &data)

	if data.Status != "ERROR" {
		t.Errorf("status: got %q, want ERROR", data.Status)
	}
	if data.ErrorCount != 1 {
		t.Errorf("error_count: got %d, want 1", data.ErrorCount)
	}
	if data.LastError != "file not found" {
		t.Errorf("last_error: got %q", data.LastError)
	}
}

func TestPermissionRequestSetsWaiting(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "perm-test"})
	HandleEvent("PermissionRequest", map[string]any{
		"session_id": "perm-test",
		"tool_name":  "Bash",
	})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("perm-test"))
	json.Unmarshal(raw, &data)

	if data.Status != "WAITING_PERMISSION" {
		t.Errorf("status: got %q, want WAITING_PERMISSION", data.Status)
	}
}

func TestStopPreservesWaitingAnswer(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "stop-test"})
	HandleEvent("Notification", map[string]any{"session_id": "stop-test"})
	// Notification should set WAITING_ANSWER when IDLE
	HandleEvent("Stop", map[string]any{"session_id": "stop-test"})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("stop-test"))
	json.Unmarshal(raw, &data)

	if data.Status != "WAITING_ANSWER" {
		t.Errorf("Stop should preserve WAITING_ANSWER, got %q", data.Status)
	}
}

func TestStopResetsToIdle(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "idle-test"})
	HandleEvent("PreToolUse", map[string]any{"session_id": "idle-test", "tool_name": "Read"})
	HandleEvent("Stop", map[string]any{"session_id": "idle-test"})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("idle-test"))
	json.Unmarshal(raw, &data)

	if data.Status != "IDLE" {
		t.Errorf("Stop should reset WORKING to IDLE, got %q", data.Status)
	}
}

func TestSubagentCounting(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "sub-test"})
	HandleEvent("SubagentStart", map[string]any{"session_id": "sub-test"})
	HandleEvent("SubagentStart", map[string]any{"session_id": "sub-test"})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("sub-test"))
	json.Unmarshal(raw, &data)

	if data.SubagentCount != 2 {
		t.Errorf("subagent_count: got %d, want 2", data.SubagentCount)
	}
}

func TestActivityHistoryShifts(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "activity-test"})

	// 3 tool uses
	for i := 0; i < 3; i++ {
		HandleEvent("PreToolUse", map[string]any{"session_id": "activity-test", "tool_name": "Read"})
	}

	var data StatusData
	raw, _ := os.ReadFile(statusPath("activity-test"))
	json.Unmarshal(raw, &data)

	if len(data.ActivityHistory) != 10 {
		t.Fatalf("expected 10 buckets, got %d", len(data.ActivityHistory))
	}

	// Last bucket should have activity
	last := data.ActivityHistory[len(data.ActivityHistory)-1]
	if last == 0 {
		t.Error("last activity bucket should be non-zero")
	}
}

func TestBootstrapWithoutSessionStart(t *testing.T) {
	setupTestEnv(t)

	// First event is PreToolUse, no prior SessionStart
	HandleEvent("PreToolUse", map[string]any{
		"session_id": "bootstrap-test",
		"tool_name":  "Bash",
	})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("bootstrap-test"))
	json.Unmarshal(raw, &data)

	if data.Status != "WORKING" {
		t.Errorf("status: got %q, want WORKING", data.Status)
	}
	if data.LastTool != "Bash" {
		t.Errorf("last_tool: got %q, want Bash", data.LastTool)
	}
}

func TestErrorMessageTruncation(t *testing.T) {
	setupTestEnv(t)

	HandleEvent("SessionStart", map[string]any{"session_id": "trunc-test"})

	longError := ""
	for i := 0; i < 600; i++ {
		longError += "x"
	}
	HandleEvent("PostToolUseFailure", map[string]any{
		"session_id": "trunc-test",
		"error":      longError,
	})

	var data StatusData
	raw, _ := os.ReadFile(statusPath("trunc-test"))
	json.Unmarshal(raw, &data)

	if len(data.LastError) > 500 {
		t.Errorf("error should be truncated to 500, got %d", len(data.LastError))
	}
}
