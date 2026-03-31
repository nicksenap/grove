package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/config"
)

var validSessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// StatusData is the state written to ~/.grove/status/{session_id}.json.
type StatusData struct {
	SessionID      string   `json:"session_id"`
	Status         string   `json:"status"`
	CWD            string   `json:"cwd,omitempty"`
	StartedAt      string   `json:"started_at,omitempty"`
	Model          string   `json:"model,omitempty"`
	LastEvent      string   `json:"last_event"`
	LastEventTime  string   `json:"last_event_time"`
	LastTool       string   `json:"last_tool,omitempty"`
	LastMessage    string   `json:"last_message,omitempty"`
	LastError      string   `json:"last_error,omitempty"`
	ToolCount      int      `json:"tool_count"`
	ErrorCount     int      `json:"error_count"`
	SubagentCount  int      `json:"subagent_count"`
	ActivityHistory []int   `json:"activity_history"`
	GitBranch      string   `json:"git_branch,omitempty"`
	GitDirtyCount  int      `json:"git_dirty_count"`
	PID            int      `json:"pid"`
	ZellijSession  string   `json:"zellij_session,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`
}

func statusDir() string {
	return filepath.Join(config.GroveDir, "status")
}

func statusPath(sessionID string) string {
	return filepath.Join(statusDir(), sessionID+".json")
}

// HandleEvent processes a Claude Code hook event.
func HandleEvent(event string, payload map[string]any) {
	sessionID, _ := payload["session_id"].(string)
	if sessionID == "" {
		return
	}
	if !validSessionID.MatchString(sessionID) {
		return
	}

	if event == "SessionEnd" {
		os.Remove(statusPath(sessionID))
		return
	}

	// Load or create status
	data := loadStatus(sessionID)
	now := time.Now().Format("2006-01-02T15:04:05")

	data.SessionID = sessionID
	data.LastEvent = event
	data.LastEventTime = now
	data.PID = os.Getppid()

	switch event {
	case "SessionStart":
		data.Status = "IDLE"
		data.StartedAt = now
		if cwd, ok := payload["cwd"].(string); ok {
			data.CWD = cwd
			data.GitBranch = getGitBranch(cwd)
			data.GitDirtyCount = getGitDirtyCount(cwd)
		}
		if model, ok := payload["model"].(string); ok {
			data.Model = model
		}
		if pm, ok := payload["permission_mode"].(string); ok {
			data.PermissionMode = pm
		}
		data.ActivityHistory = make([]int, 10)
		data.ZellijSession = os.Getenv("ZELLIJ_SESSION_NAME")

	case "PreToolUse":
		data.Status = "WORKING"
		data.ToolCount++
		if tool, ok := payload["tool_name"].(string); ok {
			data.LastTool = tool
		}
		bumpActivity(data)

	case "PostToolUse":
		data.Status = "WORKING"
		bumpActivity(data)

	case "PostToolUseFailure":
		data.Status = "ERROR"
		data.ErrorCount++
		if errMsg, ok := payload["error"].(string); ok {
			if len(errMsg) > 500 {
				errMsg = errMsg[:500]
			}
			data.LastError = errMsg
		}

	case "Stop":
		if data.Status != "WAITING_ANSWER" {
			data.Status = "IDLE"
		}
		if msg, ok := payload["last_assistant_message"].(string); ok {
			if len(msg) > 500 {
				msg = msg[:500]
			}
			data.LastMessage = msg
		}

	case "PermissionRequest":
		data.Status = "WAITING_PERMISSION"
		if tool, ok := payload["tool_name"].(string); ok {
			data.LastTool = tool
		}

	case "Notification":
		if data.Status == "IDLE" {
			data.Status = "WAITING_ANSWER"
		}

	case "UserPromptSubmit":
		data.Status = "WORKING"
		bumpActivity(data)

	case "SubagentStart":
		data.SubagentCount++

	case "SubagentStop":
		// no-op

	case "PreCompact", "TaskCompleted":
		// no-op
	}

	saveStatus(data)
}

func loadStatus(sessionID string) *StatusData {
	path := statusPath(sessionID)
	rawData, err := os.ReadFile(path)
	if err != nil {
		return &StatusData{SessionID: sessionID}
	}
	var data StatusData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return &StatusData{SessionID: sessionID}
	}
	return &data
}

func saveStatus(data *StatusData) {
	dir := statusDir()
	os.MkdirAll(dir, 0o755)

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}

	tmp := statusPath(data.SessionID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	os.Rename(tmp, statusPath(data.SessionID))
}

func bumpActivity(data *StatusData) {
	if len(data.ActivityHistory) == 0 {
		data.ActivityHistory = make([]int, 10)
	}
	// Shift left and increment last bucket
	data.ActivityHistory = append(data.ActivityHistory[1:], data.ActivityHistory[len(data.ActivityHistory)-1]+1)
}

func getGitBranch(dir string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getGitDirtyCount(dir string) int {
	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

// WriteStatusLine writes the status to stderr in a format suitable for shell prompts.
func WriteStatusLine(sessionID string) string {
	data := loadStatus(sessionID)
	if data.Status == "" {
		return ""
	}
	return fmt.Sprintf("%s [%s] %s", data.SessionID, data.Status, data.LastTool)
}
