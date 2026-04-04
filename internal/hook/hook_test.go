package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	h.HandleEvent("Notification", map[string]any{
		"session_id": "stop-test",
		"message":    "I have a question for you",
	})
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

// ---------------------------------------------------------------------------
// Tests for parity with Python hook handler
// ---------------------------------------------------------------------------

func TestSessionStartSetsProjectName(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{
		"session_id": "proj-test",
		"cwd":        "/home/user/projects/my-app",
	})

	data := readStatus(t, h, "proj-test")
	if data.ProjectName != "my-app" {
		t.Errorf("project_name: got %q, want my-app", data.ProjectName)
	}
}

func TestSessionStartRecordsSessionSource(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{
		"session_id":      "src-test",
		"source":          "vscode",
		"permission_mode": "plan",
	})

	data := readStatus(t, h, "src-test")
	if data.SessionSource != "vscode" {
		t.Errorf("session_source: got %q, want vscode", data.SessionSource)
	}
}

func TestNotificationSetsMessage(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "notif-test"})
	h.HandleEvent("Notification", map[string]any{
		"session_id": "notif-test",
		"message":    "Task completed successfully",
	})

	data := readStatus(t, h, "notif-test")
	if data.NotificationMessage == nil || *data.NotificationMessage != "Task completed successfully" {
		t.Errorf("notification_message: got %v", data.NotificationMessage)
	}
}

func TestNotificationPermissionKeyword(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "notif-perm"})
	h.HandleEvent("PreToolUse", map[string]any{"session_id": "notif-perm", "tool_name": "Bash"})
	h.HandleEvent("Notification", map[string]any{
		"session_id": "notif-perm",
		"message":    "Needs permission to proceed",
	})

	data := readStatus(t, h, "notif-perm")
	if data.Status != "WAITING_PERMISSION" {
		t.Errorf("status: got %q, want WAITING_PERMISSION", data.Status)
	}
}

func TestNotificationQuestionKeyword(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "notif-q"})
	h.HandleEvent("Notification", map[string]any{
		"session_id": "notif-q",
		"message":    "I have a question for you",
	})

	data := readStatus(t, h, "notif-q")
	if data.Status != "WAITING_ANSWER" {
		t.Errorf("status: got %q, want WAITING_ANSWER", data.Status)
	}
}

func TestPermissionRequestSetsToolSummaryBash(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "summary-bash"})
	h.HandleEvent("PermissionRequest", map[string]any{
		"session_id": "summary-bash",
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "echo hello"},
	})

	data := readStatus(t, h, "summary-bash")
	if data.ToolRequestSummary == nil || *data.ToolRequestSummary != "$ echo hello" {
		t.Errorf("tool_request_summary: got %v", data.ToolRequestSummary)
	}
}

func TestPermissionRequestSetsToolSummaryEdit(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "summary-edit"})
	h.HandleEvent("PermissionRequest", map[string]any{
		"session_id": "summary-edit",
		"tool_name":  "Edit",
		"tool_input": map[string]any{
			"file_path":  "/foo/bar.py",
			"old_string": "x = 1",
			"new_string": "x = 2",
		},
	})

	data := readStatus(t, h, "summary-edit")
	if data.ToolRequestSummary == nil {
		t.Fatal("tool_request_summary should not be nil")
	}
	summary := *data.ToolRequestSummary
	if !strings.Contains(summary, "/foo/bar.py") {
		t.Errorf("summary should contain file path, got: %s", summary)
	}
	if !strings.Contains(summary, "- x = 1") {
		t.Errorf("summary should contain old_string, got: %s", summary)
	}
	if !strings.Contains(summary, "+ x = 2") {
		t.Errorf("summary should contain new_string, got: %s", summary)
	}
}

func TestPermissionRequestSetsToolSummaryGrep(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "summary-grep"})
	h.HandleEvent("PermissionRequest", map[string]any{
		"session_id": "summary-grep",
		"tool_name":  "Grep",
		"tool_input": map[string]any{"pattern": "TODO", "path": "/src"},
	})

	data := readStatus(t, h, "summary-grep")
	if data.ToolRequestSummary == nil || *data.ToolRequestSummary != "TODO in /src" {
		t.Errorf("tool_request_summary: got %v", data.ToolRequestSummary)
	}
}

func TestPermissionRequestNoToolInput(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "summary-nil"})
	h.HandleEvent("PermissionRequest", map[string]any{
		"session_id": "summary-nil",
		"tool_name":  "Bash",
	})

	data := readStatus(t, h, "summary-nil")
	if data.ToolRequestSummary != nil {
		t.Errorf("tool_request_summary should be nil when no tool_input, got %v", data.ToolRequestSummary)
	}
}

func TestPreToolUseClearsNotification(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "clear-test"})
	h.HandleEvent("Notification", map[string]any{
		"session_id": "clear-test",
		"message":    "some notification",
	})
	h.HandleEvent("PreToolUse", map[string]any{
		"session_id": "clear-test",
		"tool_name":  "Read",
	})

	data := readStatus(t, h, "clear-test")
	if data.NotificationMessage != nil {
		t.Errorf("notification_message should be cleared on PreToolUse, got %v", data.NotificationMessage)
	}
	if data.ToolRequestSummary != nil {
		t.Errorf("tool_request_summary should be cleared on PreToolUse, got %v", data.ToolRequestSummary)
	}
}

func TestSubagentStopDecrements(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "sub-dec"})
	h.HandleEvent("SubagentStart", map[string]any{"session_id": "sub-dec", "agent_type": "Explore"})
	h.HandleEvent("SubagentStart", map[string]any{"session_id": "sub-dec", "agent_type": "Plan"})
	h.HandleEvent("SubagentStop", map[string]any{"session_id": "sub-dec", "agent_type": "Explore"})

	data := readStatus(t, h, "sub-dec")
	if data.SubagentCount != 1 {
		t.Errorf("subagent_count: got %d, want 1 after start(2)+stop(1)", data.SubagentCount)
	}
}

func TestSubagentStopFloorsAtZero(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "sub-floor"})
	h.HandleEvent("SubagentStop", map[string]any{"session_id": "sub-floor"})

	data := readStatus(t, h, "sub-floor")
	if data.SubagentCount != 0 {
		t.Errorf("subagent_count: got %d, want 0 (floor)", data.SubagentCount)
	}
}

func TestActiveSubagentsTracked(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "active-sub"})
	h.HandleEvent("SubagentStart", map[string]any{"session_id": "active-sub", "agent_type": "Explore"})
	h.HandleEvent("SubagentStart", map[string]any{"session_id": "active-sub", "agent_type": "Plan"})

	data := readStatus(t, h, "active-sub")
	if len(data.ActiveSubagents) != 2 {
		t.Errorf("active_subagents length: got %d, want 2", len(data.ActiveSubagents))
	}

	h.HandleEvent("SubagentStop", map[string]any{"session_id": "active-sub", "agent_type": "Explore"})
	data = readStatus(t, h, "active-sub")
	if len(data.ActiveSubagents) != 1 {
		t.Errorf("active_subagents length after stop: got %d, want 1", len(data.ActiveSubagents))
	}
	if len(data.ActiveSubagents) > 0 && data.ActiveSubagents[0] != "Plan" {
		t.Errorf("remaining subagent: got %q, want Plan", data.ActiveSubagents[0])
	}
}

func TestPreCompactTracksCount(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "compact-test"})
	h.HandleEvent("PreCompact", map[string]any{
		"session_id": "compact-test",
		"trigger":    "auto",
	})
	h.HandleEvent("PreCompact", map[string]any{
		"session_id": "compact-test",
		"trigger":    "manual",
	})

	data := readStatus(t, h, "compact-test")
	if data.CompactCount != 2 {
		t.Errorf("compact_count: got %d, want 2", data.CompactCount)
	}
	if data.CompactTrigger != "manual" {
		t.Errorf("compact_trigger: got %q, want manual", data.CompactTrigger)
	}
}

func TestUserPromptSubmitCapturesInitialPrompt(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "prompt-test"})
	h.HandleEvent("UserPromptSubmit", map[string]any{
		"session_id": "prompt-test",
		"prompt":     "Fix the login bug",
	})

	data := readStatus(t, h, "prompt-test")
	if data.InitialPrompt != "Fix the login bug" {
		t.Errorf("initial_prompt: got %q, want 'Fix the login bug'", data.InitialPrompt)
	}
	if data.Status != "WORKING" {
		t.Errorf("status: got %q, want WORKING", data.Status)
	}

	// Second prompt should NOT overwrite
	h.HandleEvent("UserPromptSubmit", map[string]any{
		"session_id": "prompt-test",
		"prompt":     "Also fix the signup bug",
	})
	data = readStatus(t, h, "prompt-test")
	if data.InitialPrompt != "Fix the login bug" {
		t.Errorf("initial_prompt should not change, got %q", data.InitialPrompt)
	}
}

func TestUserPromptSubmitClearsNotification(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "prompt-clear"})
	h.HandleEvent("Notification", map[string]any{
		"session_id": "prompt-clear",
		"message":    "waiting for input",
	})
	h.HandleEvent("UserPromptSubmit", map[string]any{
		"session_id": "prompt-clear",
		"prompt":     "go ahead",
	})

	data := readStatus(t, h, "prompt-clear")
	if data.NotificationMessage != nil {
		t.Errorf("notification_message should be cleared, got %v", data.NotificationMessage)
	}
	if data.ToolRequestSummary != nil {
		t.Errorf("tool_request_summary should be cleared, got %v", data.ToolRequestSummary)
	}
}

func TestBootstrapSetsProjectName(t *testing.T) {
	h := testHandler(t)

	// First event is PreToolUse (mid-session hook install)
	h.HandleEvent("PreToolUse", map[string]any{
		"session_id": "boot-proj",
		"cwd":        "/home/user/my-project",
		"tool_name":  "Read",
	})

	data := readStatus(t, h, "boot-proj")
	if data.ProjectName != "my-project" {
		t.Errorf("project_name: got %q, want my-project", data.ProjectName)
	}
}

func TestStopRecordsLastMessage(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "msg-test"})
	h.HandleEvent("Stop", map[string]any{
		"session_id":             "msg-test",
		"last_assistant_message": "I've completed the task",
	})

	data := readStatus(t, h, "msg-test")
	if data.LastMessage != "I've completed the task" {
		t.Errorf("last_message: got %q", data.LastMessage)
	}
}

func TestPostToolUseClearsNotification(t *testing.T) {
	h := testHandler(t)

	h.HandleEvent("SessionStart", map[string]any{"session_id": "post-clear"})
	h.HandleEvent("Notification", map[string]any{
		"session_id": "post-clear",
		"message":    "some notification",
	})
	h.HandleEvent("PostToolUse", map[string]any{
		"session_id": "post-clear",
	})

	data := readStatus(t, h, "post-clear")
	if data.NotificationMessage != nil {
		t.Errorf("notification_message should be cleared on PostToolUse, got %v", data.NotificationMessage)
	}
}
