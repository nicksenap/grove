package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var validSessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// StatusData is the state written to ~/.grove/status/{session_id}.json.
type StatusData struct {
	SessionID           string   `json:"session_id"`
	Status              string   `json:"status"`
	CWD                 string   `json:"cwd,omitempty"`
	ProjectName         string   `json:"project_name,omitempty"`
	StartedAt           string   `json:"started_at,omitempty"`
	Model               string   `json:"model,omitempty"`
	LastEvent           string   `json:"last_event"`
	LastEventTime       string   `json:"last_event_time"`
	LastTool            string   `json:"last_tool,omitempty"`
	LastMessage         string   `json:"last_message,omitempty"`
	LastError           string   `json:"last_error,omitempty"`
	ToolCount           int      `json:"tool_count"`
	ErrorCount          int      `json:"error_count"`
	SubagentCount       int      `json:"subagent_count"`
	CompactCount        int      `json:"compact_count"`
	ActivityHistory     []int    `json:"activity_history"`
	GitBranch           string   `json:"git_branch,omitempty"`
	GitDirtyCount       int      `json:"git_dirty_count"`
	PID                 int      `json:"pid"`
	ZellijSession       string   `json:"zellij_session,omitempty"`
	PermissionMode      string   `json:"permission_mode,omitempty"`
	SessionSource       string   `json:"session_source,omitempty"`
	InitialPrompt       string   `json:"initial_prompt,omitempty"`
	CompactTrigger      string   `json:"compact_trigger,omitempty"`
	NotificationMessage *string  `json:"notification_message"`
	ToolRequestSummary  *string  `json:"tool_request_summary"`
	ActiveSubagents     []string `json:"active_subagents"`
}

// Handler processes Claude Code hook events.
// All dependencies are injectable for testing.
type Handler struct {
	StatusDir   string
	NowFn       func() time.Time
	PidFn       func() int
	EnvFn       func(string) string
	GitBranchFn func(dir string) string
	GitDirtyFn  func(dir string) int
}

// NewHandler creates a Handler with real dependencies.
func NewHandler(groveDir string) *Handler {
	return &Handler{
		StatusDir:   filepath.Join(groveDir, "status"),
		NowFn:       time.Now,
		PidFn:       os.Getppid,
		EnvFn:       os.Getenv,
		GitBranchFn: defaultGitBranch,
		GitDirtyFn:  defaultGitDirtyCount,
	}
}

func (h *Handler) statusPath(sessionID string) string {
	return filepath.Join(h.StatusDir, sessionID+".json")
}

// HandleEvent processes a Claude Code hook event.
func (h *Handler) HandleEvent(event string, payload map[string]any) {
	sessionID, _ := payload["session_id"].(string)
	if sessionID == "" {
		return
	}
	if !validSessionID.MatchString(sessionID) {
		return
	}

	if event == "SessionEnd" {
		os.Remove(h.statusPath(sessionID))
		return
	}

	// Load or create status
	data := h.loadStatus(sessionID)
	now := h.NowFn().Format("2006-01-02T15:04:05")

	data.SessionID = sessionID
	data.LastEvent = event
	data.LastEventTime = now
	data.PID = h.PidFn()

	// Bootstrap: set project_name from cwd if missing (handles mid-session hook install)
	if cwd, ok := payload["cwd"].(string); ok && cwd != "" && data.CWD == "" {
		data.CWD = cwd
	}
	if data.ProjectName == "" && data.CWD != "" {
		data.ProjectName = path.Base(data.CWD)
	}

	// Track permission mode (available on every event)
	if pm, ok := payload["permission_mode"].(string); ok && pm != "" {
		data.PermissionMode = pm
	}

	switch event {
	case "SessionStart":
		cwd, _ := payload["cwd"].(string)
		data.Status = "IDLE"
		data.StartedAt = now
		data.CWD = cwd
		data.ProjectName = path.Base(cwd)
		if cwd != "" {
			data.GitBranch = h.GitBranchFn(cwd)
			data.GitDirtyCount = h.GitDirtyFn(cwd)
		}
		if model, ok := payload["model"].(string); ok {
			data.Model = model
		}
		if pm, ok := payload["permission_mode"].(string); ok {
			data.PermissionMode = pm
		}
		if src, ok := payload["source"].(string); ok {
			data.SessionSource = src
		}
		data.ActivityHistory = make([]int, 10)
		data.ActiveSubagents = nil
		data.NotificationMessage = nil
		data.ToolRequestSummary = nil
		data.InitialPrompt = ""
		data.CompactCount = 0
		data.CompactTrigger = ""
		data.ToolCount = 0
		data.ErrorCount = 0
		data.SubagentCount = 0
		data.ZellijSession = h.EnvFn("ZELLIJ_SESSION_NAME")

	case "PreToolUse":
		data.Status = "WORKING"
		data.ToolCount++
		data.NotificationMessage = nil
		data.ToolRequestSummary = nil
		if tool, ok := payload["tool_name"].(string); ok {
			data.LastTool = tool
		}
		bumpActivity(data)

	case "PostToolUse":
		data.Status = "WORKING"
		data.NotificationMessage = nil
		data.ToolRequestSummary = nil

	case "PostToolUseFailure":
		data.Status = "ERROR"
		data.ErrorCount++
		data.ToolRequestSummary = nil
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
		data.ToolRequestSummary = toolSummary(payload)

	case "Notification":
		msg, _ := payload["message"].(string)
		data.NotificationMessage = &msg
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "permission") {
			data.Status = "WAITING_PERMISSION"
		} else if containsAny(lower, "question", "input", "answer", "elicitation") {
			data.Status = "WAITING_ANSWER"
		}

	case "UserPromptSubmit":
		data.Status = "WORKING"
		data.NotificationMessage = nil
		data.ToolRequestSummary = nil
		if prompt, ok := payload["prompt"].(string); ok && prompt != "" && data.InitialPrompt == "" {
			if len(prompt) > 300 {
				prompt = prompt[:300]
			}
			data.InitialPrompt = prompt
		}
		bumpActivity(data)

	case "SubagentStart":
		data.SubagentCount++
		if agentType, ok := payload["agent_type"].(string); ok && agentType != "" {
			data.ActiveSubagents = append(data.ActiveSubagents, agentType)
			if len(data.ActiveSubagents) > 5 {
				data.ActiveSubagents = data.ActiveSubagents[len(data.ActiveSubagents)-5:]
			}
		}

	case "SubagentStop":
		if data.SubagentCount > 0 {
			data.SubagentCount--
		}
		if agentType, ok := payload["agent_type"].(string); ok && agentType != "" {
			for i, a := range data.ActiveSubagents {
				if a == agentType {
					data.ActiveSubagents = append(data.ActiveSubagents[:i], data.ActiveSubagents[i+1:]...)
					break
				}
			}
		}

	case "PreCompact":
		data.CompactCount++
		if trigger, ok := payload["trigger"].(string); ok {
			data.CompactTrigger = trigger
		} else {
			data.CompactTrigger = "auto"
		}

	case "TaskCompleted":
		// no-op (event + time already recorded above)
	}

	h.saveStatus(data)
}

func (h *Handler) loadStatus(sessionID string) *StatusData {
	path := h.statusPath(sessionID)
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

func (h *Handler) saveStatus(data *StatusData) {
	os.MkdirAll(h.StatusDir, 0o755)

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}

	tmp := h.statusPath(data.SessionID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	os.Rename(tmp, h.statusPath(data.SessionID))
}

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// toolSummary builds a human-readable summary of a tool request from the payload.
// Returns nil if no tool_input is present.
func toolSummary(payload map[string]any) *string {
	toolInput, ok := payload["tool_input"].(map[string]any)
	if !ok {
		return nil
	}

	toolName, _ := payload["tool_name"].(string)
	var summary string

	switch toolName {
	case "Bash":
		cmd, _ := toolInput["command"].(string)
		if cmd == "" {
			return nil
		}
		if len(cmd) > 300 {
			cmd = cmd[:300]
		}
		summary = "$ " + cmd

	case "Edit":
		fp, _ := toolInput["file_path"].(string)
		oldStr, _ := toolInput["old_string"].(string)
		newStr, _ := toolInput["new_string"].(string)
		var parts []string
		parts = append(parts, fp)
		for _, line := range strings.SplitN(oldStr, "\n", 4)[:min(3, len(strings.SplitN(oldStr, "\n", 4)))] {
			parts = append(parts, "- "+line)
		}
		for _, line := range strings.SplitN(newStr, "\n", 4)[:min(3, len(strings.SplitN(newStr, "\n", 4)))] {
			parts = append(parts, "+ "+line)
		}
		summary = strings.Join(parts, "\n")

	case "Write":
		fp, _ := toolInput["file_path"].(string)
		content, _ := toolInput["content"].(string)
		lines := len(strings.Split(content, "\n"))
		summary = fmt.Sprintf("%s (%d lines)", fp, lines)

	case "Read":
		fp, _ := toolInput["file_path"].(string)
		if fp == "" {
			return nil
		}
		summary = fp

	case "WebFetch":
		url, _ := toolInput["url"].(string)
		if url == "" {
			return nil
		}
		summary = url

	case "Grep", "Glob":
		pat, _ := toolInput["pattern"].(string)
		p, _ := toolInput["path"].(string)
		if p != "" {
			summary = pat + " in " + p
		} else {
			summary = pat
		}

	default:
		data, err := json.Marshal(toolInput)
		if err != nil {
			return nil
		}
		s := string(data)
		if len(s) > 300 {
			s = s[:300]
		}
		summary = s
	}

	return &summary
}

func bumpActivity(data *StatusData) {
	if len(data.ActivityHistory) == 0 {
		data.ActivityHistory = make([]int, 10)
	}
	// Shift left and increment last bucket
	data.ActivityHistory = append(data.ActivityHistory[1:], data.ActivityHistory[len(data.ActivityHistory)-1]+1)
}

func defaultGitBranch(dir string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func defaultGitDirtyCount(dir string) int {
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
func (h *Handler) WriteStatusLine(sessionID string) string {
	data := h.loadStatus(sessionID)
	if data.Status == "" {
		return ""
	}
	return fmt.Sprintf("%s [%s] %s", data.SessionID, data.Status, data.LastTool)
}
