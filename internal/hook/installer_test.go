package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestInstaller(t *testing.T) *Installer {
	t.Helper()
	dir := t.TempDir()
	return &Installer{
		SettingsPath: filepath.Join(dir, "settings.json"),
		NowFn:        func() time.Time { return time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC) },
	}
}

func TestInstallCreatesSettings(t *testing.T) {
	inst := newTestInstaller(t)

	count, err := inst.Install("/usr/local/bin/gw")
	if err != nil {
		t.Fatal(err)
	}
	if count != len(HookEvents) {
		t.Errorf("expected %d hooks installed, got %d", len(HookEvents), count)
	}

	// Verify file was created
	data, err := os.ReadFile(inst.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooks := settings["hooks"].(map[string]any)
	if len(hooks) != len(HookEvents) {
		t.Errorf("expected %d event types, got %d", len(HookEvents), len(hooks))
	}

	// Check one hook entry
	sessionStart := hooks["SessionStart"].([]any)
	rule := sessionStart[0].(map[string]any)
	innerHooks := rule["hooks"].([]any)
	hookEntry := innerHooks[0].(map[string]any)
	cmd := hookEntry["command"].(string)
	if cmd != "GROVE_EVENT=SessionStart /usr/local/bin/gw _hook --event SessionStart" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestInstallPreservesExistingSettings(t *testing.T) {
	inst := newTestInstaller(t)

	// Write existing settings with a non-hook key
	existing := map[string]any{
		"permissions": map[string]any{"allow": true},
	}
	data, _ := json.Marshal(existing)
	os.MkdirAll(filepath.Dir(inst.SettingsPath), 0o755)
	os.WriteFile(inst.SettingsPath, data, 0o644)

	_, err := inst.Install("/usr/local/bin/gw")
	if err != nil {
		t.Fatal(err)
	}

	// Verify existing keys preserved
	result, _ := os.ReadFile(inst.SettingsPath)
	var settings map[string]any
	json.Unmarshal(result, &settings)

	if settings["permissions"] == nil {
		t.Error("existing settings key was removed")
	}
}

func TestInstallPreservesExistingHooks(t *testing.T) {
	inst := newTestInstaller(t)

	// Write settings with an existing non-grove hook
	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Write",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "my-custom-hook",
						},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(existing)
	os.MkdirAll(filepath.Dir(inst.SettingsPath), 0o755)
	os.WriteFile(inst.SettingsPath, data, 0o644)

	_, err := inst.Install("/usr/local/bin/gw")
	if err != nil {
		t.Fatal(err)
	}

	result, _ := os.ReadFile(inst.SettingsPath)
	var settings map[string]any
	json.Unmarshal(result, &settings)

	hooks := settings["hooks"].(map[string]any)
	preToolUse := hooks["PreToolUse"].([]any)

	// Should have 2 entries: custom + grove
	if len(preToolUse) != 2 {
		t.Errorf("expected 2 PreToolUse hooks, got %d", len(preToolUse))
	}
}

func TestInstallUpdatesExistingGroveHook(t *testing.T) {
	inst := newTestInstaller(t)

	// Install once
	inst.Install("/usr/local/bin/gw")

	// Install again with different path
	_, err := inst.Install("/opt/homebrew/bin/gw")
	if err != nil {
		t.Fatal(err)
	}

	result, _ := os.ReadFile(inst.SettingsPath)
	var settings map[string]any
	json.Unmarshal(result, &settings)

	hooks := settings["hooks"].(map[string]any)
	sessionStart := hooks["SessionStart"].([]any)

	// Should still be just 1 entry, not 2
	if len(sessionStart) != 1 {
		t.Errorf("expected 1 hook (updated), got %d", len(sessionStart))
	}

	// Should use new path
	rule := sessionStart[0].(map[string]any)
	innerHooks := rule["hooks"].([]any)
	cmd := innerHooks[0].(map[string]any)["command"].(string)
	if cmd != "GROVE_EVENT=SessionStart /opt/homebrew/bin/gw _hook --event SessionStart" {
		t.Errorf("hook not updated: %s", cmd)
	}
}

func TestUninstall(t *testing.T) {
	inst := newTestInstaller(t)

	inst.Install("/usr/local/bin/gw")

	removed, err := inst.Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed != len(HookEvents) {
		t.Errorf("expected %d removed, got %d", len(HookEvents), removed)
	}

	// Hooks key should be gone
	result, _ := os.ReadFile(inst.SettingsPath)
	var settings map[string]any
	json.Unmarshal(result, &settings)

	if settings["hooks"] != nil {
		t.Error("hooks key should be removed when empty")
	}
}

func TestUninstallPreservesOtherHooks(t *testing.T) {
	inst := newTestInstaller(t)

	// Install grove hooks
	inst.Install("/usr/local/bin/gw")

	// Add a custom hook manually
	settings, _ := inst.loadSettings()
	hooks := settings["hooks"].(map[string]any)
	preToolUse := hooks["PreToolUse"].([]any)
	preToolUse = append(preToolUse, map[string]any{
		"matcher": "Write",
		"hooks": []any{
			map[string]any{"type": "command", "command": "my-hook"},
		},
	})
	hooks["PreToolUse"] = preToolUse
	settings["hooks"] = hooks
	inst.saveSettings(settings)

	removed, _ := inst.Uninstall()
	if removed != len(HookEvents) {
		t.Errorf("expected %d removed, got %d", len(HookEvents), removed)
	}

	// Custom hook should remain
	result, _ := os.ReadFile(inst.SettingsPath)
	var after map[string]any
	json.Unmarshal(result, &after)

	afterHooks := after["hooks"].(map[string]any)
	afterPTU := afterHooks["PreToolUse"].([]any)
	if len(afterPTU) != 1 {
		t.Errorf("custom hook should remain, got %d hooks", len(afterPTU))
	}
}

func TestUninstallNoFile(t *testing.T) {
	inst := newTestInstaller(t)

	removed, err := inst.Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

func TestIsInstalled(t *testing.T) {
	inst := newTestInstaller(t)

	if inst.IsInstalled() {
		t.Error("should not be installed initially")
	}

	inst.Install("/usr/local/bin/gw")

	if !inst.IsInstalled() {
		t.Error("should be installed after install")
	}

	inst.Uninstall()

	if inst.IsInstalled() {
		t.Error("should not be installed after uninstall")
	}
}

func TestBackup(t *testing.T) {
	inst := newTestInstaller(t)

	// No file — no backup
	path, err := inst.Backup()
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Error("should return empty path when no settings file")
	}

	// Create a settings file
	os.MkdirAll(filepath.Dir(inst.SettingsPath), 0o755)
	os.WriteFile(inst.SettingsPath, []byte(`{"foo": "bar"}`), 0o644)

	path, err = inst.Backup()
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected backup path")
	}

	// Verify backup content matches
	original, _ := os.ReadFile(inst.SettingsPath)
	backup, _ := os.ReadFile(path)
	if string(original) != string(backup) {
		t.Error("backup content doesn't match original")
	}

	// Verify backup filename contains timestamp
	expected := inst.SettingsPath + ".bak.20260404_120000"
	if path != expected {
		t.Errorf("expected backup at %s, got %s", expected, path)
	}
}
