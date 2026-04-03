package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	os.MkdirAll(groveDir, 0o755)
	return &Handler{
		StatusDir:   filepath.Join(groveDir, "status"),
		NowFn:       time.Now,
		PidFn:       func() int { return 12345 },
		EnvFn:       func(string) string { return "" },
		GitBranchFn: func(string) string { return "main" },
		GitDirtyFn:  func(string) int { return 0 },
	}
}

func readStatus(t *testing.T, h *Handler, sessionID string) StatusData {
	t.Helper()
	var data StatusData
	raw, _ := os.ReadFile(h.statusPath(sessionID))
	json.Unmarshal(raw, &data)
	return data
}

func TestSessionStartCreatesFile(t *testing.T) {
	h := testHandler(t)

	payload := map[string]any{
		"session_id": "test-123",
		"cwd":        t.TempDir(),
		"model":      "claude-4",
	}
	h.HandleEvent("SessionStart", payload)

	path := h.statusPath("test-123")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("status file should exist")
	}

	data := readStatus(t, h, "test-123")
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

func TestSessionStartRecordsPID(t *testing.T) {
	h := testHandler(t)
	h.PidFn = func() int { return 99999 }

	h.HandleEvent("SessionStart", map[string]any{"session_id": "pid-test"})

	data := readStatus(t, h, "pid-test")
	if data.PID != 99999 {
		t.Errorf("PID: got %d, want 99999", data.PID)
	}
}

func TestSessionStartRecordsZellijSession(t *testing.T) {
	h := testHandler(t)
	h.EnvFn = func(key string) string {
		if key == "ZELLIJ_SESSION_NAME" {
			return "my-session"
		}
		return ""
	}

	h.HandleEvent("SessionStart", map[string]any{"session_id": "zellij-test"})

	data := readStatus(t, h, "zellij-test")
	if data.ZellijSession != "my-session" {
		t.Errorf("zellij_session: got %q, want my-session", data.ZellijSession)
	}
}

func TestSessionStartRecordsGitBranch(t *testing.T) {
	h := testHandler(t)
	h.GitBranchFn = func(string) string { return "feat/cool" }
	h.GitDirtyFn = func(string) int { return 3 }

	h.HandleEvent("SessionStart", map[string]any{
		"session_id": "git-test",
		"cwd":        "/some/dir",
	})

	data := readStatus(t, h, "git-test")
	if data.GitBranch != "feat/cool" {
		t.Errorf("git_branch: got %q, want feat/cool", data.GitBranch)
	}
	if data.GitDirtyCount != 3 {
		t.Errorf("git_dirty_count: got %d, want 3", data.GitDirtyCount)
	}
}

func TestSessionStartRecordsTimestamp(t *testing.T) {
	h := testHandler(t)
	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	h.NowFn = func() time.Time { return fixed }

	h.HandleEvent("SessionStart", map[string]any{"session_id": "time-test"})

	data := readStatus(t, h, "time-test")
	if data.StartedAt != "2025-06-15T12:00:00" {
		t.Errorf("started_at: got %q", data.StartedAt)
	}
}

func TestSessionEndRemovesFile(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "end-test"})
	h.HandleEvent("SessionEnd", map[string]any{"session_id": "end-test"})

	path := h.statusPath("end-test")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("status file should be removed")
	}
}

func TestInvalidSessionIDRejected(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "../../../etc/passwd"})
	path := h.statusPath("../../../etc/passwd")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("should reject path traversal session IDs")
	}
}

func TestEmptySessionIDRejected(t *testing.T) {
	h := testHandler(t)
	h.HandleEvent("SessionStart", map[string]any{"session_id": ""})
	// No crash = pass
}

func TestPreToolUseSetsWorking(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "tool-test"})
	h.HandleEvent("PreToolUse", map[string]any{
		"session_id": "tool-test",
		"tool_name":  "Read",
	})

	data := readStatus(t, h, "tool-test")
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
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "err-test"})
	h.HandleEvent("PostToolUseFailure", map[string]any{
		"session_id": "err-test",
		"error":      "file not found",
	})

	data := readStatus(t, h, "err-test")
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
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "perm-test"})
	h.HandleEvent("PermissionRequest", map[string]any{
		"session_id": "perm-test",
		"tool_name":  "Bash",
	})

	data := readStatus(t, h, "perm-test")
	if data.Status != "WAITING_PERMISSION" {
		t.Errorf("status: got %q, want WAITING_PERMISSION", data.Status)
	}
}

func TestStopPreservesWaitingAnswer(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "stop-test"})
	h.HandleEvent("Notification", map[string]any{"session_id": "stop-test"})
	h.HandleEvent("Stop", map[string]any{"session_id": "stop-test"})

	data := readStatus(t, h, "stop-test")
	if data.Status != "WAITING_ANSWER" {
		t.Errorf("Stop should preserve WAITING_ANSWER, got %q", data.Status)
	}
}

func TestStopResetsToIdle(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "idle-test"})
	h.HandleEvent("PreToolUse", map[string]any{"session_id": "idle-test", "tool_name": "Read"})
	h.HandleEvent("Stop", map[string]any{"session_id": "idle-test"})

	data := readStatus(t, h, "idle-test")
	if data.Status != "IDLE" {
		t.Errorf("Stop should reset WORKING to IDLE, got %q", data.Status)
	}
}

func TestSubagentCounting(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "sub-test"})
	h.HandleEvent("SubagentStart", map[string]any{"session_id": "sub-test"})
	h.HandleEvent("SubagentStart", map[string]any{"session_id": "sub-test"})

	data := readStatus(t, h, "sub-test")
	if data.SubagentCount != 2 {
		t.Errorf("subagent_count: got %d, want 2", data.SubagentCount)
	}
}

func TestActivityHistoryShifts(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "activity-test"})
	for i := 0; i < 3; i++ {
		h.HandleEvent("PreToolUse", map[string]any{"session_id": "activity-test", "tool_name": "Read"})
	}

	data := readStatus(t, h, "activity-test")
	if len(data.ActivityHistory) != 10 {
		t.Fatalf("expected 10 buckets, got %d", len(data.ActivityHistory))
	}
	last := data.ActivityHistory[len(data.ActivityHistory)-1]
	if last == 0 {
		t.Error("last activity bucket should be non-zero")
	}
}

func TestBootstrapWithoutSessionStart(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("PreToolUse", map[string]any{
		"session_id": "bootstrap-test",
		"tool_name":  "Bash",
	})

	data := readStatus(t, h, "bootstrap-test")
	if data.Status != "WORKING" {
		t.Errorf("status: got %q, want WORKING", data.Status)
	}
	if data.LastTool != "Bash" {
		t.Errorf("last_tool: got %q, want Bash", data.LastTool)
	}
}

func TestErrorMessageTruncation(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "trunc-test"})

	longError := ""
	for i := 0; i < 600; i++ {
		longError += "x"
	}
	h.HandleEvent("PostToolUseFailure", map[string]any{
		"session_id": "trunc-test",
		"error":      longError,
	})

	data := readStatus(t, h, "trunc-test")
	if len(data.LastError) > 500 {
		t.Errorf("error should be truncated to 500, got %d", len(data.LastError))
	}
}
