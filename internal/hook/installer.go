package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HookEvents lists all Claude Code hook event types.
var HookEvents = []string{
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"Stop",
	"SessionStart",
	"SessionEnd",
	"Notification",
	"PermissionRequest",
	"UserPromptSubmit",
	"SubagentStart",
	"SubagentStop",
	"PreCompact",
	"TaskCompleted",
}

const groveMarker = "gw _hook"

// Installer manages Claude Code hook registration in settings.json.
type Installer struct {
	SettingsPath string
	NowFn        func() time.Time
}

// NewInstaller creates an Installer pointing at ~/.claude/settings.json.
func NewInstaller() *Installer {
	home, _ := os.UserHomeDir()
	return &Installer{
		SettingsPath: filepath.Join(home, ".claude", "settings.json"),
		NowFn:        time.Now,
	}
}

// ResolveGW finds the gw binary path.
func ResolveGW() (string, error) {
	path, err := exec.LookPath("gw")
	if err != nil {
		return "", fmt.Errorf("gw not found on PATH; install Grove first")
	}
	return path, nil
}

// IsInstalled checks if Grove hooks are present in settings.json.
func (inst *Installer) IsInstalled() bool {
	settings, err := inst.loadSettings()
	if err != nil {
		return false
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}

	for _, eventHooks := range hooks {
		rules, ok := eventHooks.([]any)
		if !ok {
			continue
		}
		for _, rule := range rules {
			if inst.ruleIsGrove(rule) {
				return true
			}
		}
	}
	return false
}

// Install registers Grove hooks in settings.json.
// Returns the number of hooks installed and any error.
func (inst *Installer) Install(gwPath string) (int, error) {
	settings, err := inst.loadSettings()
	if err != nil {
		settings = map[string]any{}
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
	}

	count := 0
	for _, event := range HookEvents {
		command := fmt.Sprintf("GROVE_EVENT=%s %s _hook --event %s", event, gwPath, event)
		ruleEntry := map[string]any{
			"matcher": "",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": command,
				},
			},
		}

		eventHooks, _ := hooks[event].([]any)

		// Find existing grove rule
		groveIdx := -1
		for i, rule := range eventHooks {
			if inst.ruleIsGrove(rule) {
				groveIdx = i
				break
			}
		}

		if groveIdx >= 0 {
			eventHooks[groveIdx] = ruleEntry
		} else {
			eventHooks = append(eventHooks, ruleEntry)
			count++
		}

		hooks[event] = eventHooks
	}

	settings["hooks"] = hooks

	if err := inst.saveSettings(settings); err != nil {
		return 0, err
	}

	if count == 0 {
		count = len(HookEvents) // all updated
	}
	return count, nil
}

// Uninstall removes all Grove hooks from settings.json.
// Returns the number of hooks removed.
func (inst *Installer) Uninstall() (int, error) {
	settings, err := inst.loadSettings()
	if err != nil {
		return 0, nil
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return 0, nil
	}

	removed := 0
	for event, eventHooksRaw := range hooks {
		eventHooks, ok := eventHooksRaw.([]any)
		if !ok {
			continue
		}

		var filtered []any
		for _, rule := range eventHooks {
			if inst.ruleIsGrove(rule) {
				removed++
			} else {
				filtered = append(filtered, rule)
			}
		}

		if len(filtered) > 0 {
			hooks[event] = filtered
		} else {
			delete(hooks, event)
		}
	}

	if len(hooks) > 0 {
		settings["hooks"] = hooks
	} else {
		delete(settings, "hooks")
	}

	if removed > 0 {
		if err := inst.saveSettings(settings); err != nil {
			return 0, err
		}
	}
	return removed, nil
}

// Backup creates a timestamped backup of settings.json.
// Returns the backup path, or empty string if no settings file exists.
func (inst *Installer) Backup() (string, error) {
	if _, err := os.Stat(inst.SettingsPath); os.IsNotExist(err) {
		return "", nil
	}

	ts := inst.NowFn().Format("20060102_150405")
	backupPath := inst.SettingsPath + ".bak." + ts

	src, err := os.Open(inst.SettingsPath)
	if err != nil {
		return "", fmt.Errorf("opening settings: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("creating backup: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("copying settings: %w", err)
	}

	return backupPath, nil
}

func (inst *Installer) ruleIsGrove(rule any) bool {
	ruleMap, ok := rule.(map[string]any)
	if !ok {
		return false
	}
	innerHooks, ok := ruleMap["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range innerHooks {
		hMap, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hMap["command"].(string)
		if strings.Contains(cmd, groveMarker) {
			return true
		}
	}
	return false
}

func (inst *Installer) loadSettings() (map[string]any, error) {
	data, err := os.ReadFile(inst.SettingsPath)
	if err != nil {
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing settings.json: %w", err)
	}
	return settings, nil
}

func (inst *Installer) saveSettings(settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(inst.SettingsPath), 0o755); err != nil {
		return err
	}

	tmp := inst.SettingsPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, inst.SettingsPath)
}
